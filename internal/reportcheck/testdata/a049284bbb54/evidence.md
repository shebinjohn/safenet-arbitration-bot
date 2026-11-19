# Evidence report: request 0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27

## 1. Request

- Request ID: `0xa049284bbb5473c8994e5160c9006c60a4d651545b8d09990581f6d92d1b0f27`, state `FROZEN` (arbitration frozenBlock 49199453, deadline 49683293, outcome `pending`). Source: request file.
- Safe: `0xc4994fe4f501a02434ca7f25b14fd803f5d7a388` on chain ID 1 (Ethereum Mainnet).
- Proposal time: 2026-11-10T01:26:25Z (Gnosis block 49199445, tx `0x58b5f675b8a12ffa11931416843a139daa181a32a071770db4f05854cb251b60`).
- `safeBlock` 26381211, `ethereumBlock` 26381211.
- Charter: `charter.safenet-gov.eth`, CID `bafkreigg6ajqdb52kb644wcxegurt7r6jlclru4mjy3lwxkbh6avm3vgre`.
- Disputed rules: R-4.3 only.
- safeTxHash: `0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f`; `oracleData` is `0x` (no protocol-recorded purpose, § 2.11).

## 2. Transaction

Source: request file `proposal.transaction`, and `arbot calls -json -request-file` (as of block 26381211).

- One call (kind `root`), nonce 17:
  - to `0xdac17f958d2ee523a2206206994597c13d831ec7` (code hash `0xb44fb4e949d0f78f87f79ee46428f23a2a5713ce6fc6e0beb3dda78c2ac1ea55`, code size 11075, not the Safe, not a § 2.1 contract), value 0, operation CALL;
  - data `0xa9059cbb000000000000000000000000c01ea83bacda63097627f45d987684569fdceae50000000000000000000000000000000000000000000000000000000000989680`, decoded by `arbot` as `transfer(address,uint256)` with `to` = `0xc01ea83bacda63097627f45d987684569fdceae5` and `value` = 10000000.
- Refund parameters: safeTxGas 0, baseGas 0, gasPrice 0, gasToken `0x0000000000000000000000000000000000000000`, refundReceiver `0x0000000000000000000000000000000000000000`. `arbot calls` lists no refund call.
- The fee token of the request (USDC.e) is unrelated to the Safe's transaction.

## 3. Leads

Source: request file `votes`. Not evidence.

- `0x101d59dbb8c5e70f45d52634f23800fca4fbe031`: APPROVED, empty reason.
- `0x6d081448c04d9ef7cf8ecf54207b1931ecc25312`: DENIED, reason `R-4.3`, cites R-4.3.
- `0xc84a0928632382e75c543acced101b9525027362`: APPROVED, empty reason.

## 4. Evidence

### R-4.3 / § 2.3, § 2.4: the value recipient

- The only call is `transfer` on `0xdac17f958d2ee523a2206206994597c13d831ec7` to recipient `0xc01ea83bacda63097627f45d987684569fdceae5`, amount 10000000 in the token's smallest unit (`arbot calls`). `arbot calls` identifies the Safe as a proxy version 1.4.1 with singleton `0x41675c099f32341bf84bfc5382af534df5c7461a`, singleton version 1.4.1, as of 26381211. The token's decimals and symbol are not reported by `arbot` (see Gaps).

### R-4.3 / § 2.4: the Safe's history on Ethereum Mainnet

`arbot transfers -json -request-file` (ERC-20 transfers out of the Safe, blocks 26281212 to 26381211, which is the last 100,000 blocks up to `safeBlock`):

| Block | Tx | Token | To | Value |
|---|---|---|---|---|
| 26288585 | `0x8b2eea6d27f292a43708566b23f7915473f238588c93c10d487a6f7197b3d712` | `0xdac17f958d2ee523a2206206994597c13d831ec7` | `0x799973ccdcf61c8a1c944e31e4779c51f6030e8c` | 19990000000 |
| 26288588 | `0x7bfce1904095f9389e946a3b74f9afc6194e742eb3d67404b27d2c6b9c1d9d35` | same token | `0xa73dc770e02f9b803d0b538443b28b71d75da25b` | 1490000000 |
| 26288595 | `0x4aed73a3adcfa94e482181868bb448d3dc95c902f666d0b370d629229c5ca780` | same token | `0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae` | 14004150000 |
| 26288599 | `0xb7ee5512e90647f5eb3d3a4e695859a9c316e02279eb16be3e20e26222f795fb` | same token | `0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae` | 5996250000 |
| 26381211 | `0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5` | same token | `0xc01ea83bacda63097627f45d987684569fdceae5` | 10000000 |

- The last row is the transaction under arbitration itself (see Excluded). Apart from it, no ERC-20 transfer to `0xc01ea83bacda63097627f45d987684569fdceae5` from the Safe appears in the scanned range.

`arbot executions -json -calls -request-file` (executions by the Safe, blocks 26281212 to 26381211), nine in total, all `success`, payment 0:

| Nonce | Block | Source | Call(s) |
|---|---|---|---|
| 9 | 26288439 | calldata | root call to the Safe itself, no data, value 0 |
| 10 | 26288445 | calldata | same |
| 11 | 26288452 | calldata | same |
| 12 | 26288457 | calldata | same |
| 13 | 26288585 | trace | root `transfer` on `0xdac17f958d2ee523a2206206994597c13d831ec7` to `0x799973ccdcf61c8a1c944e31e4779c51f6030e8c`, 19990000000 |
| 14 | 26288588 | trace | root `transfer` on the same token to `0xa73dc770e02f9b803d0b538443b28b71d75da25b`, 1490000000 |
| 15 | 26288595 | trace | DELEGATECALL to `0x9641d764fc13c8b624c04430c7356c1c7c8102e2` (data `0x8d80ff0a`), decoded as two batched calls: `approve(0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae, 14004150000)` on the same token, then a call to `0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae` with value 18511172643600 wei and no data |
| 16 | 26288599 | trace | same shape: `approve(0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae, 5996250000)`, then a call to `0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae` with value 18511172643600 wei |
| 17 | 26381211 | trace | the transaction under arbitration (safeTxHash `0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f`), see Excluded |

- Nonce 17 is the request's nonce, and the nonces before it in the range are 9 to 16. The Safe made no executions before nonce 9 within the scanned range.

`arbot transfers -in -json -request-file` (transfers into the Safe, same range): four `native` entries, each from and to the Safe itself, value 0 (blocks 26288439, 26288445, 26288452, 26288457; the txs of nonces 9 to 12). No other inbound ERC-20 or native transfer, including none from `0xc01ea83bacda63097627f45d987684569fdceae5`, in the range.

### R-4.3 / § 2.11: protocol-recorded purpose

- `oracleData` is `0x`, so the proposal records no purpose.

### Follow-up 1 (evidence requests 1)

#### R-4.3 / § 2.4: the Safe's settings as of `safeBlock` 26381211 (`arbot safe -json -request-file`)

- Singleton `0x41675c099f32341bf84bfc5382af534df5c7461a`; owners (ownerCount 3): `0x0cf777780ea18a90d52e7c78d3a1c4c26b7274fa`, `0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98`, `0x0ae40c604a661c09405c35a6f8b88760f28a068f`; threshold 2; enabled modules: none; fallback handler `0xfd0732dc9e303f09fcef3a7388ad10a83459ec99`; guard and module guard: zero address.
- `0xc01ea83bacda63097627f45d987684569fdceae5` is not in the owner list and there are no modules.
- The nonce reported is 18, as of `safeBlock` 26381211, which is the block of the proposal's own execution (nonce 17), so it includes that execution (see Excluded).

#### R-4.3 / § 2.4: the recipient's code (`arbot signatures -json -request-file ... 0xc01ea83bacda63097627f45d987684569fdceae5`, as of 26381211)

- codeHash `0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470`, codeSize 0, status `no-code`. It has no code as of `safeBlock`, so it is not a Safe or a contract of a § 2.1 version.

#### R-4.3 / § 2.4: the Safe's full history of executed Safe transactions (`arbot history -json -calls -request-file`)

- Source: Safe Transaction Service `https://api.safe.global/tx-service/eth`, observed 2026-11-10T10:08:01Z (an offchain source, § 3.4). `arbot` reports that each transaction is confirmed onchain (hash recomputed, ExecutionSuccess/Failure log found). Nonce at `safeBlock`: 18; missing nonces: none; rejected: none. It lists nonces 0 to 16, all `success`, payment 0, all with a single decoded root call or batch; the request's own execution (nonce 17) is reported separately as `proposalExecution`.

| Nonce | Block | Tx | Target / call (as `arbot` decodes it) |
|---|---|---|---|
| 0 | 25469065 | `0xb5ff5ec5cff23389e581c086f53557a844137e4eb7188ece1d6f95f3a24040c8` | CALL `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48` `transfer(0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98, 10000000)` |
| 1 | 26129835 | `0x3ff5aa721aa77b4007d29bf01e50cfe07fc07b182a793dee0f2034762fafb737` | CALL `0xdac17f958d2ee523a2206206994597c13d831ec7` `transfer(0x41656ccfa87ae0f55b6e5429724aed8d662554c4, 10000000)` |
| 2 | 26194280 | `0x43ca024904e59e90bda2479e6a31722eaa8c3ca0ed02dec3a7f5d8c1e083fa92` | same token `transfer(0x41656ccfa87ae0f55b6e5429724aed8d662554c4, 59173220000)` |
| 3 | 26252321 | `0x7150f2395ff03e3347733d17931e99106ef8136012f49897cdc06fd4c9ec2588` | `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48` `transfer(0xe408d47669c065887c1441080094afeb7b03b574, 10000000)` |
| 4 | 26252483 | `0x23efdb90a99a2f52bf62fa91251e63c598a250db28c414a9c2355671cfcefbfe` | same token `transfer(0xe408d47669c065887c1441080094afeb7b03b574, 49990000000)` |
| 5 | 26280276 | `0xc42015f015492eb90a2cb9048b02fec5dc169671d732cbfa3969b0c4ebbebd8b` | `0xdac17f958d2ee523a2206206994597c13d831ec7` `transfer(0x799973ccdcf61c8a1c944e31e4779c51f6030e8c, 10000000)` |
| 6 | 26280281 | `0xd3e8841c1b8487a6d053af1cf972d5186ab84e77530d493ad02b0f1a9762ef5d` | same token `transfer(0xa73dc770e02f9b803d0b538443b28b71d75da25b, 10000000)` |
| 7 | 26280752 | `0x019b58977e4ea2f647fb1f8341a1e1abbd8150ddee51a29b237ec5211c9dfb61` | DELEGATECALL `0x9641d764fc13c8b624c04430c7356c1c7c8102e2`, batched: `approve(0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae, 10040000)` on the `0xdac1...` token, then a call to `0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae`, value 0, no data |
| 8 | 26280811 | `0x326f661836ac54e55e983f78692d17f4e20b125b0ee8bfe4f80797d40d09d88e` | same shape, `approve(0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae, 10030000)`, then call to `0x1231...` value 0 |
| 9-12 | 26288439-26288457 | see above | empty call to the Safe itself |
| 13-16 | 26288585-26288599 | see above | as in the earlier table (transfers to `0x7999...`, `0xa73d...`, approvals and value calls to `0x1231...`) |

- Across nonces 0 to 16, `0xc01ea83bacda63097627f45d987684569fdceae5` is not the target, a recipient, or an approved spender of any decoded call. The nonces 0 to 8 are before block 26281212, so these are the executions the 100,000-block scan did not reach (nonce 5 to 8 are in blocks 26280276 to 26280811, which are earlier than 26281212).
- The amounts in nonces 0, 1, 3, 5, and 6 are each 10000000 in the token's smallest unit, the same amount as the proposed transfer. Nonces 5 and 6 are to `0x7999...` and `0xa73d...`, the recipients of nonces 13 and 14.

#### Coverage of transfers outside the Safe's executions

- `arbot history` covers only the Safe's own executions. ERC-20 transfers into or out of the Safe that were not made by its executions are covered only by the 100,000-block scans in section 4 (blocks 26281212 to 26381211, none involving `0xc01ea83bacda63097627f45d987684569fdceae5`). I ran wider scans (`arbot transfers -to 0xc01e... -blocks 1000000`, `arbot executions -blocks 1000000` and `-blocks 300000`); the 1,000,000-block executions scan failed after 30 minutes with `eth_getLogs ... connection reset by peer` (https://eth.drpc.org), and I stopped the other scans for time, so they gave no result.

#### Gaps added

- Transfers into or out of the Safe, other than through its executions, before block 26281212 (`arbot transfers -blocks N`, `-in`): not established because the wide scans failed or were stopped. Any inbound transfer from `0xc01ea83bacda63097627f45d987684569fdceae5`, or direct token movement by an allowance, before that block is unknown.

## Follow-up 2 (evidence requests 4)

Observed 2026-11-10, Ethereum Mainnet, as of `safeBlock` 26381211.

### Request 4: the token of each 10000000 payment (`arbot history -json -calls -request-file`, Safe Transaction Service, confirmed onchain; missing nonces: none; rejected: none; nonce at `safeBlock` 18)

- Nonce 0 (block 25469065): `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48` `transfer(0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98, 10000000)`.
- Nonce 1 (26129835): `0xdac17f958d2ee523a2206206994597c13d831ec7` `transfer(0x41656ccfa87ae0f55b6e5429724aed8d662554c4, 10000000)`.
- Nonce 3 (26252321): `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48` `transfer(0xe408d47669c065887c1441080094afeb7b03b574, 10000000)`.
- Nonce 5 (26280276): `0xdac17f958d2ee523a2206206994597c13d831ec7` `transfer(0x799973ccdcf61c8a1c944e31e4779c51f6030e8c, 10000000)`.
- Nonce 6 (26280281): `0xdac17f958d2ee523a2206206994597c13d831ec7` `transfer(0xa73dc770e02f9b803d0b538443b28b71d75da25b, 10000000)`.
- So nonces 0 and 3 are on `0xa0b8...eb48`, and nonces 1, 5 and 6 on `0xdac1...1ec7`. Nonces 2 and 4 are later amounts to the recipients of nonces 1 and 3: 59173220000 on `0xdac1...1ec7` to `0x41656ccfa87ae0f55b6e5429724aed8d662554c4`, and 49990000000 on `0xa0b8...eb48` to `0xe408d47669c065887c1441080094afeb7b03b574`.

### Request 5: the nonce-0 owner

- `0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98` (an owner per `arbot safe`) is the recipient of the nonce-0 `transfer` only. In nonces 1 to 16 it is not the target, a recipient, or a spender of any decoded call (`arbot history`). `0xc01ea83bacda63097627f45d987684569fdceae5` is not the target, recipient, or spender of any call in nonces 0 to 16.

### Whole-history scans on `https://rpc.mevblocker.io`

Each was run as `arbot transfers -json -blocks 1000000 -request-file .msgboard/a049284bbb54/request.json`, succeeded on the first attempt, and covered blocks 25381212 to 26381211 (`safeBlock`). The range starts 87,853 blocks before the Safe's first execution (nonce 0, block 25469065). Earlier attempts on other RPCs (eth.drpc.org: connection resets, 429, 408; rpc.flashbots.net: `pruned history unavailable`) produced nothing and are superseded.

- `-in -from 0xc01ea83bacda63097627f45d987684569fdceae5` (request 1): no transfers, ERC-20 or native, from that address into the Safe in blocks 25381212 to 26381211.
- `-to 0xc01ea83bacda63097627f45d987684569fdceae5` (request 3): one ERC-20 transfer out of the Safe to it, the transaction under arbitration (block 26381211, tx `0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5`, log index 97, token `0xdac17f958d2ee523a2206206994597c13d831ec7`, value 10000000), kept under Excluded. No other ERC-20 transfer from the Safe to it in the range.
- `-to 0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98` (request 5): one ERC-20 transfer out of the Safe to it: block 25469065, tx `0xb5ff5ec5cff23389e581c086f53557a844137e4eb7188ece1d6f95f3a24040c8`, log index 538, token `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`, value 10000000 (the nonce-0 payment). No later ERC-20 transfer from the Safe to it in the range.
- The `-to` scans list ERC-20 `Transfer` events only; native and ERC-721 transfers out of the Safe are not listed.

### Request 2: the Safe's creation block

- `arbot` has no command that reports the Safe's creation (proxy creation) block or its first inbound funding block (gap). The earliest block it gives for any activity of the Safe is 25469065 (nonce 0, `arbot history`); the scans above reach back to 25381212 and show no earlier ERC-20 transfer out of the Safe or inbound transfer from `0xc01e...` in that range.

## Follow-up 3 (Safe creation)

Source: `arbot history -json -request-file .msgboard/a049284bbb54/request.json`, field `creation`, observed 2026-11-10T12:15:41Z from the Safe Transaction Service `https://api.safe.global/tx-service/eth` (an offchain source, § 3.4), as of `safeBlock` 26381211. `arbot` reports it as confirmed onchain: no code at the block before, code at the block, and the factory is the emitter of the account's `ProxyCreation` log.

- Creation block: 25468989.
- Creation transaction: `0x7529568fd7668adc16c8bc55df229f784f53c74ad7769e6b746b57071c4ba40f`.
- Factory: `0x4e1dcf7ad4e460cfd30791ccc4f9c8a4f820ec67`.
- The whole-history scans of Follow-up 2 (blocks 25381212 to 26381211) start 87,777 blocks before the creation block (25468989 - 25381212). The Safe's first execution (nonce 0, block 25469065) is 76 blocks after the creation block.
- The Safe's address had no code before block 25468989, and the scans did not cover blocks before 25381212, so transfers to the address before block 25381212 were not searched.

## 5. Gaps

- The Safe's creation block is established (Follow-up 3: block 25468989). Not established: transfers to the Safe's address before block 25381212, when it had no code, and its first inbound funding block (no `arbot` command provides it).
- Transfers into or out of the Safe, other than through its executions, before block 25381212: not scanned. Between blocks 25381212 and 26381211 they are covered by the mevblocker scans for `0xc01ea83bacda63097627f45d987684569fdceae5` (inbound from it, outbound to it) and for `0x66cd5fa074c477bd8e730f0fa2ace2a0b998fe98` (outbound to it).
- Native currency and ERC-721 transfers out of the Safe are not listed by `arbot transfers` (§ 2.4).
- History of the recipient `0xc01ea83bacda63097627f45d987684569fdceae5` itself (its age, activity, and other counterparties), which bears on "legitimate onchain history" and address poisoning under R-4.3. No `arbot` command reports an arbitrary account's transaction history, nor its resemblance to other addresses beyond the Safe's own lists above.
- The token `0xdac17f958d2ee523a2206206994597c13d831ec7`: symbol, decimals, supply (no `arbot` command reports it for a non-Safe-involved token), so the human-readable size of 10000000 is not given.
- Public offchain context and security evidence (§ 3.4) on the recipient, such as labels, scam flags, or an address-poisoning report: no `arbot` command collects it, and none was gathered.
- Standard-user behavior for comparable transactions (§ 2.9): no comparison data available from `arbot`.

## 6. Excluded

- The execution at block 26381211 (tx `0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5`, emitter of `ExecutionSuccess` the Safe, `txHash` `0xb683dc4eb2de43bbf46e56099ca40c29db5fdccd70e64692049e58a5688aa00f`, payment 0; `arbot events` shows an ERC-20 `Transfer` of 10000000 from the Safe to `0xc01ea83bacda63097627f45d987684569fdceae5` at log index 97) has the same safeTxHash as the proposed transaction. It is the effect of the transaction itself, so § 3.6 excludes it as evidence of the history before the proposal. It is reported only because `arbot` lists it among the executions up to `safeBlock` (26381211), which is also the block that the execution is in. Its effects were not used above to describe the Safe's earlier history, and the table rows for it are marked.
- No private or after-the-fact intent claims were found in the request.
- The `-to 0xc01ea83bacda63097627f45d987684569fdceae5` scan of Follow-up 2 (blocks 25381212 to 26381211) lists the transaction under arbitration itself: block 26381211, tx `0x513798b039db59b78959ead457fcaa39456dfa68a7ef1bee28a674b8d03e41f5`, log index 97. It is the effect of the transaction itself (§ 3.6) and is not used as history.
