package safesettings

import (
	"context"

	"github.com/safe-research/safenet-arbitration-bot/internal/checks"
	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
)

// The reasons why an Account has no settings.
const (
	// NoCode is the reason of an account without code, such as one that is
	// controlled by a private key, or a contract that isn't deployed yet.
	NoCode = "no-code"
	// NotASafeProxy is the reason of an account whose code isn't that of a
	// SafeProxy of a § 2.1 proxy version on its network.
	NotASafeProxy = "not-a-safe-proxy"
	// UnknownSingleton is the reason of a SafeProxy whose singleton isn't a Safe or
	// SafeL2 singleton of a § 2.1 version, whose storage layout is unknown.
	UnknownSingleton = "unknown-singleton"
)

// Reasons describe each reason why an Account has no settings, for people.
var Reasons = map[string]string{
	NoCode:           "the account has no code, so it isn't a Safe",
	NotASafeProxy:    "the account's code isn't that of a SafeProxy of a § 2.1 proxy version on its network",
	UnknownSingleton: "the account's singleton isn't a Safe singleton of a § 2.1 version, so its storage layout is unknown",
}

// Account is an account on a chain at a block, with what § 2.1 identifies a
// Safe by, as arbot calls reports the Safe of a request, and its settings if it
// is a Safe of a § 2.1 version.
type Account struct {
	checks.SafeAccount
	Block uint64 `json:"block"`
	// Settings are the account's settings, or nil if it isn't a Safe of a § 2.1
	// version, since its storage then means something else, or nothing.
	Settings *Settings `json:"settings"`
	// Reason is why Settings is nil, one of NoCode, NotASafeProxy, and
	// UnknownSingleton, and empty otherwise.
	Reason string `json:"reason,omitempty"`
}

// ReadAccount reads the account at address at block, and its settings if it is
// a Safe of a § 2.1 version: if its code is that of a SafeProxy of a § 2.1
// proxy version on its network, and its singleton is a Safe or SafeL2 singleton
// of a § 2.1 version, as the checks identify a Safe. The storage of any other
// account isn't read as settings, since zeros would read as a Safe without
// owners rather than as no Safe.
func ReadAccount(ctx context.Context, client *ethrpc.Client, address ethrpc.Address, block ethrpc.BlockNumber) (*Account, error) {
	safe, err := checks.NewCallReader(client, address, block).Safe(ctx)
	if err != nil {
		return nil, err
	}
	a := &Account{SafeAccount: safe, Block: uint64(block)}
	switch {
	case safe.CodeSize == 0:
		a.Reason = NoCode
	case safe.ProxyVersion == nil:
		a.Reason = NotASafeProxy
	case safe.SingletonVersion == nil:
		a.Reason = UnknownSingleton
	default:
		if a.Settings, err = Read(ctx, client, address, block); err != nil {
			return nil, err
		}
	}
	return a, nil
}
