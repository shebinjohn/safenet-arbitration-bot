# Evidence `arbot` couldn't provide

**Priority:** decoding calls through proxies/facets and tuples, and the arguments
or logs of a simulation trace, decided three "not accepted" outcomes in a row
(2d01, c3f9, ffa3).

Each gap lists where it mattered and a proposed command for the `add-evidence` skill.

## Decoding tuple and struct arguments; simulation logs and transfer recipients

- **Mattered in:** `0x2d0174daa69d…` (decided the outcome: no case accepted). The
  swap's `beneficiary` and `partnerAndFee` are tuple fields of
  `swapExactAmountInOnCurveV1`; `calls -abi` and `simulate` don't decode tuples, and
  the simulation trace shows no transfer recipients.
- **Proposed:** decode tuple/struct arguments from a verified ABI in `arbot calls
  -abi` and `arbot simulate`, and have `arbot simulate -json` list the logs the
  simulated call emits, with ERC-20 `Transfer` events decoded (token, from, to,
  value).

## View calls and storage reads on any contract

- **Mattered in:** d39d, f8a4, b2aa (vault `pendingSilo`, roles, claimable
  amounts, `whitelistManager`); 2d01 (router owner, facets).
- **Proposed:** `arbot view -request-file <path> [-block N] <address> <signature>
  [args...]` (eth_call of a view function at `safeBlock`, decoded by the signature),
  and `arbot storage -request-file <path> [-block N] <address> <slot>`.

## Time of a past block

- **Mattered in:** several reports needed timestamps for history facts.
- **Proposed:** `arbot block -request-file <path> [-chain safe|ethereum|gnosis]
  <number>` printing the block's number, hash and time.

## Public security flags or labels (§ 2.7)

- **Mattered in:** `0x3296fc227b79…` (decisive and open; drives "human review"),
  `0x863d07a43e2c…` (flags on recipient unchecked).
- **Proposed:** `arbot labels -request-file <path> <address>`, reading a trusted,
  free public label/flag source with URL and observation time. No suitable provider
  identified yet.

## Decoding a proxy call with its implementation's ABI

- **Mattered in:** `0xffa3c8072266…` (G1/G2: Pendle RouterV4 facet
  `exitPostExpToToken` `receiver` undecoded; decided the outcome), `0xc3f9a39d441f…` (G1/G3: `depositForBurn` arguments on a
  CCTP `TokenMessengerV2` proxy; `calls -abi` uses only the proxy's own ABI).
- **Proposed:** `arbot calls -abi` and `arbot simulate -trace` follow a proxy to the
  implementation (EIP-1967 slot or trace delegatecall) and decode with its verified
  ABI; or an `-abi-from <address>` flag.

## Reading proxy storage slots (EIP-1967 implementation/admin)

- **Mattered in:** `0xc3f9a39d441f…` (G2; the judge named the spender's admin as the
  onchain link that could settle § 2.4).
- **Proposed:** `arbot storage -request-file <path> <address> <slot>` with named
  slots `eip1967.implementation` / `eip1967.admin`, plus `arbot view` for `owner()`.

## `arbot explorer` block ranges

- **Mattered in:** `0xc3f9a39d441f…` (G4/G10: Etherscan listings capped at 10000,
  no `-from-block`/`-to-block`, so a pre-`safeBlock` usage window can't be isolated).
- **Proposed:** `-from-block`/`-to-block` on `arbot explorer txs` and `tokentx`.
