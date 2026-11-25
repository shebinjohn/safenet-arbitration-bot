# Evidence `arbot` couldn't provide

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
