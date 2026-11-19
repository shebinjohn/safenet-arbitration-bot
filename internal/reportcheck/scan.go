package reportcheck

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sources are the values of the files that a report may cite.
type sources struct {
	// hex are the digits of the hex values, in lowercase and without 0x, of at
	// least 8 digits.
	hex    []string
	joined string
	// abbreviations are the hex values that a source itself abbreviates, as their
	// digits before and after the ellipsis.
	abbreviations [][2]string
	numbers       map[string]bool
	times         map[string]bool
}

// readSources reads the values of the source files in dir for the file with the
// name, and returns them with the names of the files that it read. It fails if
// dir has no request.
func readSources(dir, name string) (*sources, []string, error) {
	src := &sources{numbers: make(map[string]bool), times: make(map[string]bool)}
	var files []string
	for _, pattern := range sourcePatterns {
		paths, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, nil, err
		}
		slices.Sort(paths)
		if len(paths) == 0 && pattern == "request.json" {
			return nil, nil, fmt.Errorf("%s: %w", filepath.Join(dir, pattern), fs.ErrNotExist)
		}
		for _, path := range paths {
			base := filepath.Base(path)
			if base == name || name != ReportFile && (strings.HasPrefix(base, "case-") || strings.HasPrefix(base, "judgment-")) {
				continue
			}
			content, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, nil, err
			}
			src.add(string(content))
			files = append(files, base)
		}
	}
	return src, files, nil
}

// add adds the values of a source file.
func (s *sources) add(text string) {
	for _, v := range scanHex(text) {
		switch {
		case v.abbreviated:
			s.abbreviations = append(s.abbreviations, [2]string{v.prefix, v.suffix})
		case len(v.prefix) >= 8:
			s.addHex(v.prefix)
		}
	}
	for _, t := range times.FindAllString(text, -1) {
		s.times[strings.ToUpper(t)] = true
	}
	for _, n := range scanNumbers(text, 1) {
		s.numbers[n.value] = true
	}
}

// addHex adds the digits of a hex value.
func (s *sources) addHex(digits string) {
	s.hex = append(s.hex, digits)
	s.joined = ""
}

// hasHex reports whether a hex value has the digits, whole or as part of it, as
// an address is part of the data of a call that passes it.
func (s *sources) hasHex(digits string) bool {
	if s.joined == "" {
		s.joined = strings.Join(s.hex, " ")
	}
	return strings.Contains(s.joined, digits)
}

// matchAbbreviation returns a hex value that starts with prefix and ends with
// suffix, and that is longer than both, of the length if it isn't zero, or ""
// if there is none.
func (s *sources) matchAbbreviation(prefix, suffix string, length int) string {
	for _, h := range s.hex {
		if len(h) > len(prefix)+len(suffix) && (length == 0 || len(h) == length) &&
			strings.HasPrefix(h, prefix) && strings.HasSuffix(h, suffix) {
			return h
		}
	}
	return ""
}

// hasAbbreviation reports whether the sources have a value that the
// abbreviation with the prefix and suffix shortens: a value that starts with
// prefix and ends with suffix, or an address or a 32-byte word in other data
// that does, such as a salt in the data of a call, or for an abbreviation
// without a suffix, data that has prefix anywhere, such as the start of a
// contract's creation code. A source that abbreviates the value itself, at
// least as far, has it too.
func (s *sources) hasAbbreviation(prefix, suffix string) bool {
	if suffix == "" && s.hasHex(prefix) || s.matchAbbreviation(prefix, suffix, 0) != "" {
		return true
	}
	if s.hasHex(prefix) {
		for i := 0; ; {
			j := strings.Index(s.joined[i:], prefix)
			if j < 0 {
				break
			}
			i += j
			for _, length := range []int{40, 64} {
				if end := i + length; length > len(prefix)+len(suffix) && end <= len(s.joined) &&
					isHex(s.joined[i:end]) && strings.HasSuffix(s.joined[i:end], suffix) {
					return true
				}
			}
			i++
		}
	}
	for _, a := range s.abbreviations {
		if strings.HasPrefix(a[0], prefix) && strings.HasSuffix(a[1], suffix) {
			return true
		}
	}
	return false
}

// hexValue is a hex value in a text, such as 0x1235…4306.
type hexValue struct {
	// text is the value as the text has it.
	text string
	// prefix is the value's digits, in lowercase and without 0x, or for an
	// abbreviation, those before the ellipsis, and suffix those after it.
	prefix, suffix string
	abbreviated    bool
	// start and end are the value's byte offsets in the text.
	start, end int
}

// requestID reports whether the value can be a request ID: a hash, or an
// abbreviation of any value.
func (v hexValue) requestID() bool {
	return v.abbreviated && len(v.prefix) >= 4 || !v.abbreviated && len(v.prefix) == 64
}

var (
	hexStart  = regexp.MustCompile(`0[xX][0-9a-fA-F]*`)
	hexDigits = regexp.MustCompile(`^[0-9a-fA-F]*`)
)

// scanHex returns the hex values in text that start with 0x, and those
// abbreviated with an ellipsis ("…" or "..."), such as 0x1235…4306 or
// 0xce3b5448….
func scanHex(text string) []hexValue {
	var values []hexValue
	for _, loc := range hexStart.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if before, _ := utf8.DecodeLastRuneInString(text[:start]); isWord(before) {
			continue
		}
		v := hexValue{prefix: strings.ToLower(text[start+2 : end]), start: start}
		for _, ellipsis := range []string{"…", "..."} {
			if rest, ok := strings.CutPrefix(text[end:], ellipsis); ok {
				v.abbreviated = true
				v.suffix = strings.ToLower(hexDigits.FindString(rest))
				end += len(ellipsis) + len(v.suffix)
				break
			}
		}
		// A word such as 0xdeadbeefzz isn't hex.
		if after, _ := utf8.DecodeRuneInString(text[end:]); isWord(after) {
			continue
		}
		v.text, v.end = text[start:end], end
		values = append(values, v)
	}
	return values
}

// maskHex returns the line with the hex values blanked out, so that their
// digits aren't read as numbers.
func maskHex(line string, values []hexValue) string {
	b := []byte(line)
	for _, v := range values {
		for i := v.start; i < v.end; i++ {
			b[i] = ' '
		}
	}
	return string(b)
}

// blank returns spaces as long as s, to blank out a match and keep the offsets
// of the rest.
func blank(s string) string {
	return strings.Repeat(" ", len(s))
}

var (
	// times matches an RFC 3339 time, as arbot writes them.
	times = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:\d{2})?`)
	// dates matches a date without a time, whose year isn't a value to check.
	dates = regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}\b|` +
		`\b(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}\b|` +
		`\b\d{1,2}\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?,?\s+\d{4}\b`)
	// derivedWord marks every number of its line as derived.
	derivedWord = regexp.MustCompile(`(?i)\bderived\b`)
	// approximate marks the number right after it as derived.
	approximate = regexp.MustCompile(`(?i)(?:\b(?:about|approximately|approx\.|roughly|around|nearly|almost)|~|≈)\s*$`)
)

// number is a number in a text.
type number struct {
	// text is the number as the text has it, and value is its digits, without
	// thousands separators.
	text, value string
	// start is the byte offset of the number in the text.
	start int
}

// separators are the thousands separators that a number can have.
var separators = []string{",", "_", " ", " ", " "}

// scanNumbers returns the numbers of text with at least minDigits digits before
// any decimal point, with or without thousands separators. Digits that are part
// of a word, such as 0x12 or ERC-20, or of a version, such as 1.4.1, aren't
// numbers.
func scanNumbers(text string, minDigits int) []number {
	var numbers []number
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !isDigit(r) {
			i += size
			continue
		}
		before, n := utf8.DecodeLastRuneInString(text[:i])
		beforeThat, _ := utf8.DecodeLastRuneInString(text[:i-min(i, n)])
		// The digits after an ellipsis end an abbreviated hex value, such as …4306.
		skip := isWord(before) || before == '.' || before == '…' || before == '-' && unicode.IsLetter(beforeThat)

		j := digitsEnd(text, i)
		digits := text[i:j]
		if j-i <= 3 {
			// Thousands separators group 3 digits after the first 1 to 3.
		groups:
			for {
				for _, sep := range separators {
					rest, ok := strings.CutPrefix(text[j:], sep)
					if ok && len(rest) >= 3 && digitsEnd(rest, 0) == 3 {
						digits += rest[:3]
						j += len(sep) + 3
						continue groups
					}
				}
				break
			}
		}
		value := digits
		if strings.HasPrefix(text[j:], ".") && j+1 < len(text) && isDigit(rune(text[j+1])) {
			k := digitsEnd(text, j+1)
			value += text[j:k]
			if strings.HasPrefix(text[k:], ".") && k+1 < len(text) && isDigit(rune(text[k+1])) {
				skip = true // a version, such as 1.4.1
			}
			j = k
		}
		if after, _ := utf8.DecodeRuneInString(text[j:]); isWord(after) {
			skip = true
		}
		if skip {
			// Skip the rest of the word, so that its digits aren't read either.
			for j < len(text) {
				r, size := utf8.DecodeRuneInString(text[j:])
				if !isWord(r) && r != '.' {
					break
				}
				j += size
			}
		} else if len(digits) >= minDigits {
			numbers = append(numbers, number{text: text[i:j], value: value, start: i})
		}
		i = j
	}
	return numbers
}

// isHex reports whether s has only hex digits.
func isHex(s string) bool {
	return strings.Trim(s, "0123456789abcdef") == ""
}

// digitsEnd returns the offset of the end of the ASCII digits of s from i.
func digitsEnd(s string, i int) int {
	for i < len(s) && isDigit(rune(s[i])) {
		i++
	}
	return i
}

func isDigit(r rune) bool {
	return '0' <= r && r <= '9'
}

// isWord reports whether r is part of a word, which a value can't be part of.
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
