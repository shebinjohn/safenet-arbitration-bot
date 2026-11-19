package create

import (
	"testing"

	"github.com/safe-research/safenet-arbitration-bot/internal/ethrpc"
	"github.com/safe-research/safenet-arbitration-bot/internal/keccak256"
)

func TestAddress(t *testing.T) {
	// The addresses that 0x6ac7…dbf0 creates with its first nonces, as computed in
	// https://ethereum.stackexchange.com/a/761, and those that the helper
	// 0xe530…8d42 that arbitration cbfadf6b9595's Safe deployed created with nonces
	// 1 to 3, as the callTracer of an archive node reports them.
	deployer := ethrpc.MustParseAddress("0x6ac7ea33f8831ea9dcc53393aaa88b25a785dbf0")
	helper := ethrpc.MustParseAddress("0xe530c247341e857042FdFf953E73Dd6c580f8D42")
	for _, test := range []struct {
		deployer ethrpc.Address
		nonce    uint64
		want     string
	}{
		{deployer, 0, "0xcd234a471b72ba2f1ccf0a70fcaba648a5eecd8d"},
		{deployer, 1, "0x343c43a37d37dff08ae8c4a11544c718abb4fcf8"},
		{deployer, 2, "0xf778b86fa74e846c4f0a1fbd1335fe81c00a0c91"},
		{deployer, 3, "0xfffd933a0bc612844eaf0c6fe3e5b8e9b6c1d19c"},
		{helper, 1, helperCreations[0]},
		{helper, 2, helperCreations[1]},
		{helper, 3, helperCreations[2]},
	} {
		if got := Address(test.deployer, test.nonce); got != ethrpc.MustParseAddress(test.want) {
			t.Errorf("Address(%s, %d) = %s, want %s", test.deployer, test.nonce, got, test.want)
		}
	}
}

// TestAddressEncoding checks the RLP encoding of nonces of each length against
// the hash that it must have: one byte below 0x80, which is its own encoding,
// and longer nonces, which have a length prefix.
func TestAddressEncoding(t *testing.T) {
	deployer := ethrpc.Address{19: 1}
	for _, test := range []struct {
		nonce   uint64
		encoded []byte
	}{
		{0, []byte{0x80}},
		{0x7f, []byte{0x7f}},
		{0x80, []byte{0x81, 0x80}},
		{0x100, []byte{0x82, 0x01, 0x00}},
		{1<<64 - 1, []byte{0x88, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
	} {
		payload := append(append([]byte{0x94}, deployer[:]...), test.encoded...)
		hash := keccak256.Hash([]byte{0xc0 + byte(len(payload))}, payload)
		if got, want := Address(deployer, test.nonce), ethrpc.Address(hash[12:]); got != want {
			t.Errorf("Address(%s, %#x) = %s, want %s", deployer, test.nonce, got, want)
		}
	}
}

func TestAddress2(t *testing.T) {
	// Examples 0, 1, and 5 of EIP-1014.
	for _, test := range []struct {
		deployer, salt, initCode, want string
	}{
		{"0x0000000000000000000000000000000000000000", "0x0000000000000000000000000000000000000000000000000000000000000000", "0x00", "0x4D1A2e2bB4F88F0250f26Ffff098B0b30B26BF38"},
		{"0xdeadbeef00000000000000000000000000000000", "0x0000000000000000000000000000000000000000000000000000000000000000", "0x00", "0xB928f69Bb1D91Cd65274e3c79d8986362984fDA3"},
		{"0x00000000000000000000000000000000deadbeef", "0x00000000000000000000000000000000000000000000000000000000cafebabe", "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "0x1d8bfDC5D46DC4f61D6b6115972536eBE6A8854C"},
	} {
		var initCode ethrpc.Bytes
		if err := initCode.UnmarshalText([]byte(test.initCode)); err != nil {
			t.Fatal(err)
		}
		deployer, salt := ethrpc.MustParseAddress(test.deployer), ethrpc.MustParseHash(test.salt)
		if got := Address2(deployer, salt, keccak256.Hash(initCode)); got != ethrpc.MustParseAddress(test.want) {
			t.Errorf("Address2(%s, %s, keccak256(%s)) = %s, want %s", deployer, salt, test.initCode, got, test.want)
		}
	}
}

// helperCreations are the contracts that the helper 0xe530…8d42 created in the
// Safe transaction of arbitration cbfadf6b9595, with CREATE and its nonces 1 to
// 3, in the CREATE frames that the callTracer of debug_traceCall reports for it
// at the request's Safe block, 26432178 on Ethereum Mainnet.
var helperCreations = []string{
	"0x3Da548EE3C9d7fb614C869e53ffdaa1a07b8A0a0",
	"0x47C6CE915E24e50A5aa98e10d310968a554Ee8af",
	"0x2505Ac2e825f8a65e9B2393E65c4A940d52a9385",
}
