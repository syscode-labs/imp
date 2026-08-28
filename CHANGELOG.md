# Changelog

## [0.8.0](https://github.com/syscode-labs/imp/compare/v0.7.0...v0.8.0) (2026-08-28)


### Features

* **api:** add ImpNetworkAttachment CRD with LAN/VLAN allowlist types ([8ea0f3f](https://github.com/syscode-labs/imp/commit/8ea0f3f500da1facd844285f117cc3f0de0adbdc))
* **network:** add RBAC-gated LAN/VLAN attachment for microVMs ([035fa58](https://github.com/syscode-labs/imp/commit/035fa580da98f03029373702d4a60a416178aac5))
* **network:** gate LAN/VLAN attachment behind admission and node bindings ([42745c6](https://github.com/syscode-labs/imp/commit/42745c63fb77018d624ca7bbb13268f2ebe8499a))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([53259a8](https://github.com/syscode-labs/imp/commit/53259a8b553a3e360fd2984e2a14ce308ab65c91))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([b2087aa](https://github.com/syscode-labs/imp/commit/b2087aa22dc4c1242efd9eb3823410ab15c889dc))
* **scheduling:** memory-pressure guardrails ([729a3df](https://github.com/syscode-labs/imp/commit/729a3dffbeabd8186c1a185748d15157a0b87a7a))
* **scheduling:** memory-pressure guardrails — QoS/priority classes, scheduling reserve, opt-in pressure lifecycle ([185e492](https://github.com/syscode-labs/imp/commit/185e4925bad017cfef30e8cefc7a3fa230347f8e))


### Bug Fixes

* **agent:** scope wasLast correctly for egress-deny cleanup ([b96f8bc](https://github.com/syscode-labs/imp/commit/b96f8bc8f5ddb3687f885232d9e266f1222971ae))
* **charts:** grant agent read on impnetworkattachments ([a3051ae](https://github.com/syscode-labs/imp/commit/a3051ae895982cd09d1049c716a00685774d5cfb))
* **cnidetect:** stop spawning cached DaemonSet informer ([e848b67](https://github.com/syscode-labs/imp/commit/e848b67e74045f82ea8d28d6e0ded162475b2138))
* correct RBAC order for leases (match controller-gen output) ([0b0f9c6](https://github.com/syscode-labs/imp/commit/0b0f9c67c0e064f11eabbf74e363fb27cf1d845e))
* **e2e:** pin operator replicas to one on single-node kind clusters ([2375165](https://github.com/syscode-labs/imp/commit/2375165eb5b8b6a7237c202e5a112dc31280ade2))
* **guest:** use overflow-safe exit-code conversion (gosec G115) ([72ead23](https://github.com/syscode-labs/imp/commit/72ead23173b58fdc3677339fe644a96c8c85fc88))
* **sandbox:** reconcile firewall on existing ImpNetwork (was create-only, left empty deny list) ([4f69a1b](https://github.com/syscode-labs/imp/commit/4f69a1b5a0815f5457ba5d44b512817b3107816a))

## [0.7.0](https://github.com/syscode-labs/imp/compare/v0.6.3...v0.7.0) (2026-08-25)


### Features

* **charts:** operator HA — 2 replicas, PDB, required anti-affinity ([29fa1a7](https://github.com/syscode-labs/imp/commit/29fa1a78bfd10d1bc6911eb58727c0e126d77e18))
* **charts:** operator HA (replicas, PDB, anti-affinity) ([24aeebb](https://github.com/syscode-labs/imp/commit/24aeebbd1214889e9b8650f95c372090890ff5b4))

## [0.6.3](https://github.com/syscode-labs/imp/compare/v0.6.2...v0.6.3) (2026-08-25)


### Bug Fixes

* **agent:** resolve VTEP IP via direct client at startup ([906bf04](https://github.com/syscode-labs/imp/commit/906bf04c7fc5ed10c9067293206af2aee6feea72))
* **agent:** run CPU-model patch after cache sync ([4ebb4cf](https://github.com/syscode-labs/imp/commit/4ebb4cfb04c5f5e32cbf3704da3d797f28c0a3d8))
* **charts:** grant imp-runtime read on impnetworks ([8e97d81](https://github.com/syscode-labs/imp/commit/8e97d81c7fc76326b841a7a0801e267e4f08ce36))
* **ci:** publish amd64 runtime images ([#51](https://github.com/syscode-labs/imp/issues/51)) ([e75941d](https://github.com/syscode-labs/imp/commit/e75941d5dd648f7e5056e120d59934b47e387ee2))
* **e2e:** import strings for BeforeSuite node labeling ([c3afc89](https://github.com/syscode-labs/imp/commit/c3afc89b30ab4121a61fe3b22ac71395ac0ff02a))
* **e2e:** point runtime DaemonSet at the locally built image ([5260cc1](https://github.com/syscode-labs/imp/commit/5260cc109331ec5123bdfdc374770f3da9252d36))
* **e2e:** resolve agent startup and node scheduling race ([3f36a16](https://github.com/syscode-labs/imp/commit/3f36a164b7c934c1e13d391af58165df8d5e6ec2))
* **e2e:** resolve agent startup and node scheduling race ([1d6efef](https://github.com/syscode-labs/imp/commit/1d6efefa221262d256c76364c93c7cd253afc422))
* **network:** coordinate IP claims across nodes ([#52](https://github.com/syscode-labs/imp/issues/52)) ([3cdc503](https://github.com/syscode-labs/imp/commit/3cdc5034aa54ecdeba8369545ed68910bf1ac84e))
* **rbac:** commit operator leases rule missed by darwin generation ([d2ef6eb](https://github.com/syscode-labs/imp/commit/d2ef6eb1172d12eeec3e267a5793696a96ef24bb))
* **runtime:** allow network lookup ([#48](https://github.com/syscode-labs/imp/issues/48)) ([5a9f3ef](https://github.com/syscode-labs/imp/commit/5a9f3efc825df8ac4bca2f2eac0406ce1ca35604))

## [0.6.2](https://github.com/syscode-labs/imp/compare/v0.6.1...v0.6.2) (2026-08-24)


### Bug Fixes

* **release:** align release-please manifest ([#46](https://github.com/syscode-labs/imp/issues/46)) ([e8e4a51](https://github.com/syscode-labs/imp/commit/e8e4a51ce57cc41ff6f5d473b2e585bad8bd38d1))

## [0.6.1](https://github.com/syscode-labs/imp/compare/v0.6.0...v0.6.1) (2026-08-24)


### Bug Fixes

* **charts:** support digest-pinned Imp images ([a1d5842](https://github.com/syscode-labs/imp/commit/a1d5842eef45caa39a534beaea97a3446948fb33))
* **charts:** support Talos extension host paths ([77b641f](https://github.com/syscode-labs/imp/commit/77b641f41931e1f6ed319bd384c3fd431cf5b584))
* **charts:** support Talos host paths and digest pins ([f0c1170](https://github.com/syscode-labs/imp/commit/f0c11704a101aaf97ed6ce61f0298942548674a5))

## [0.6.0](https://github.com/syscode-labs/imp/compare/v0.5.0...v0.6.0) (2026-07-29)


### Features

* **agent:** scale-to-zero wake-on-traffic datapath (Phase 3) ([#29](https://github.com/syscode-labs/imp/issues/29)) ([edb6352](https://github.com/syscode-labs/imp/commit/edb6352638387e2ec1f71fc385070d98b20e5557))
* **api:** add ScaleToZero desiredState mode + idleTimeout (Phase 3 foundation) ([#28](https://github.com/syscode-labs/imp/issues/28)) ([20be988](https://github.com/syscode-labs/imp/commit/20be988a464e4a630d6ccfa43c56b18ab8455810))
* **hack:** add Object Storage overlay to avoid ~GBP 0.90/month custom image cost ([1b038a4](https://github.com/syscode-labs/imp/commit/1b038a40efb6ecd19586e6bdc1da374375c664e6))
* Phase 2 — suspend-on-idle for ImpVM ([#24](https://github.com/syscode-labs/imp/issues/24)) ([b9fc5c5](https://github.com/syscode-labs/imp/commit/b9fc5c586913c2b3bc2256f1f9799eec19ed9bcd))
* **scheduler:** overcommit suspended VMs' freed capacity (Phase 2 Task 4) ([#25](https://github.com/syscode-labs/imp/issues/25)) ([086a928](https://github.com/syscode-labs/imp/commit/086a928966d6445010995ef25f71309e89798a5e))


### Bug Fixes

* **agent:** deliver firecracker binary via hostPath, wire FC_BIN + KVM preflight ([c3ba72d](https://github.com/syscode-labs/imp/commit/c3ba72d49b417059f7f8bce2b9c9e8c4bf60f455)), closes [#33](https://github.com/syscode-labs/imp/issues/33)
* **agent:** fix AF_PACKET wake hook so scale-to-zero actually wakes ([#35](https://github.com/syscode-labs/imp/issues/35)) ([a0ccf8c](https://github.com/syscode-labs/imp/commit/a0ccf8c83c294b2189e043b51ec41fc6eb624456))
* **charts:** resync drifted CRDs from base + add make sync-chart-crds ([#31](https://github.com/syscode-labs/imp/issues/31)) ([c905d33](https://github.com/syscode-labs/imp/commit/c905d33125c31246fc6d63d5ce1119fe8cd5f47a))
* **phase2:** address review findings — log casing, reserved/resident split, tests ([#26](https://github.com/syscode-labs/imp/issues/26)) ([b70d94e](https://github.com/syscode-labs/imp/commit/b70d94e76665613db888093a6374eb1ad69052eb))

## [0.5.0](https://github.com/syscode-labs/imp/compare/v0.4.0...v0.5.0) (2026-03-24)


### Features

* **agent:** use group CIDR from network status when VM has spec.networkGroup set ([14a9201](https://github.com/syscode-labs/imp/commit/14a92010c724919c70df5a095f92a1476103e464))
* **api:** add GroupCIDR type and ImpNetworkStatus.GroupCIDRs field ([d2958f6](https://github.com/syscode-labs/imp/commit/d2958f6ac0ab6039110eb8d2cab12536f5dbe8f7))
* **controller:** add carveGroupCIDRs pure function for network group CIDR allocation ([a4fc2a0](https://github.com/syscode-labs/imp/commit/a4fc2a0f82c600ec5c3a2a3ff7b64059c9339a14))
* **controller:** reconcile group CIDRs into ImpNetwork status ([8a071c7](https://github.com/syscode-labs/imp/commit/8a071c766c8bb8d422a8210f181138e4e47d9ce7))

## [0.4.0](https://github.com/syscode-labs/imp/compare/v0.3.2...v0.4.0) (2026-03-18)


### Features

* **agent:** short-circuit Cilium IPAM resolution when Cidr override is set ([1405000](https://github.com/syscode-labs/imp/commit/1405000aebeaadb85cb3f42bd486e7fd6fe9c052))
* **api:** add CiliumIPAMSpec.Cidr override field ([f93f83a](https://github.com/syscode-labs/imp/commit/f93f83af1492549b90b1babfffd2de5e01ef1b0b))
* **api:** add ImpVMSpec.RescheduleOnNodeLoss opt-in field ([d57cfac](https://github.com/syscode-labs/imp/commit/d57cfacda5807d0027b0714a763a1636dfa49c8a))
* **controller:** auto-create CiliumPodIPPool as owned child of ImpNetwork ([3b1848a](https://github.com/syscode-labs/imp/commit/3b1848a31e967d579e682fd3b86d11eab6f47e97))
* **controller:** reschedule persistent VM on node loss when RescheduleOnNodeLoss=true and no PVC ([6226982](https://github.com/syscode-labs/imp/commit/622698246325ab80a9b08508107a16140d0d880b))
* **crd:** add cidr field to CiliumIPAMSpec in generated CRD manifest ([89feb5a](https://github.com/syscode-labs/imp/commit/89feb5af14770383d689089caf7baf5a21172b82))
* **rbac:** grant operator CiliumPodIPPool CRUD ([9df428a](https://github.com/syscode-labs/imp/commit/9df428a67d4ec209e2b2a10c794deb5452dd0c6b))


### Bug Fixes

* **controller:** delete CiliumPodIPPool on ImpNetwork deletion and strengthen noop test ([ccfb574](https://github.com/syscode-labs/imp/commit/ccfb5747d98c449601284b0880b6858d7f7fc0c6))

## [0.3.2](https://github.com/syscode-labs/imp/compare/v0.3.1...v0.3.2) (2026-03-17)


### Bug Fixes

* **examples:** use curl for port-forward readiness check (cross-platform) ([71b151c](https://github.com/syscode-labs/imp/commit/71b151c7fd84b29af1d794ff12987c88126ceb92))

## [0.3.1](https://github.com/syscode-labs/imp/compare/v0.3.0...v0.3.1) (2026-03-13)


### Bug Fixes

* **ci:** address lint deprecation and retry interval tests ([b89e84f](https://github.com/syscode-labs/imp/commit/b89e84fe33da8bfa86c7e2f0e4870f4430b2cdb6))
* **ci:** use legacy recorder API with staticcheck waiver ([fc25e7c](https://github.com/syscode-labs/imp/commit/fc25e7cb3dcebadc6b3136dc946632c79e72bd76))
* **examples:** add required diskGiB to tiny-smoke class ([8c2d17e](https://github.com/syscode-labs/imp/commit/8c2d17ed2d6c6f495ab845cf11b9100e1dd3ee40))
* **examples:** make tiny-smoke connectivity check bounded and app-agnostic ([91e3050](https://github.com/syscode-labs/imp/commit/91e305028b1a8e7ee8add170b0cdfb5812238978))
* **examples:** wait for agent port-forward before exec ([d518dfd](https://github.com/syscode-labs/imp/commit/d518dfd4031821ded418f9261fa785a4a4e99d88))

## [0.3.0](https://github.com/syscode-labs/imp/compare/v0.2.2...v0.3.0) (2026-03-12)


### Features

* **examples:** add tiny smoke assets and validation ([37667bd](https://github.com/syscode-labs/imp/commit/37667bd9ec951cdb7dca0cdcde8e456d91758ed7))


### Bug Fixes

* **agent:** cleanup stale vsock sockets on stop/start paths ([7817371](https://github.com/syscode-labs/imp/commit/7817371bbc646d3be8545bdc3be57d82769a1987))
* **agent:** propagate vm env into init and guest-agent ([9c44325](https://github.com/syscode-labs/imp/commit/9c44325eadca1e7037c3b9539a0c69607eb08793))
* **agent:** reduce reconcile churn with configurable retry interval ([5059cf4](https://github.com/syscode-labs/imp/commit/5059cf42228ac077e617becbcf5c8a39e5ef9739))
* **chart:** default agent root security context and daemonset rbac ([b17743f](https://github.com/syscode-labs/imp/commit/b17743f4bc6b2d6f1902f46b5eab4315abe14e8c))
* **controller:** fail fast on invalid ImpVM reference wiring ([c258c24](https://github.com/syscode-labs/imp/commit/c258c244bd78517d31d046c8df24350c1e5b4d9d))
* **lifecycle:** clarify one-shot completion semantics ([5d978ba](https://github.com/syscode-labs/imp/commit/5d978bae49afbf7b9d5186f10a9612105f9b5195))
* **rootfs:** preserve absolute symlinks during layer extract ([98dc836](https://github.com/syscode-labs/imp/commit/98dc836a0f33ad35bd897509ec48807866fbb701))
* **rootfs:** preserve tar hardlinks during layer extract ([b106962](https://github.com/syscode-labs/imp/commit/b10696272a6a3e8d765ae7193cfb191fd2c5d101))

## [0.2.2](https://github.com/syscode-labs/imp/compare/v0.2.1...v0.2.2) (2026-03-12)


### Bug Fixes

* **runtime:** close bootstrap gaps for clean micro1 validation ([c954eb2](https://github.com/syscode-labs/imp/commit/c954eb277c9df09dcd115d05110e394eac06cffd))

## [0.2.1](https://github.com/syscode-labs/imp/compare/v0.2.0...v0.2.1) (2026-03-12)


### Bug Fixes

* **operator:** support running with webhooks disabled ([0ac8288](https://github.com/syscode-labs/imp/commit/0ac82881db8f9340c231a312f67105200b3d09c4))

## [0.2.0](https://github.com/syscode-labs/imp/compare/v0.1.0...v0.2.0) (2026-03-12)


### Features

* add microvm expireAfter with pool/template chain ([2707f16](https://github.com/syscode-labs/imp/commit/2707f169f44eb86b2d337f483d05deeb82127191))
* enforce 60s minimum for expireAfter ([4d08136](https://github.com/syscode-labs/imp/commit/4d08136edd2b08bc9f7846e7eb87ec6b9aa66f82))
* restore scaling-only runner pool design and examples ([861fa84](https://github.com/syscode-labs/imp/commit/861fa84e7130af8b453f43d11abdb66acad299e2))


### Bug Fixes

* **ci:** stabilize lint and nightly kind smoke workflows ([1b17e4f](https://github.com/syscode-labs/imp/commit/1b17e4fea2f23179033505ae59dc2468e4747814))
* **ci:** stabilize scaling.mode CEL validation rule ([8ea8f23](https://github.com/syscode-labs/imp/commit/8ea8f235cdca2dc6e5d7c7da857a72850797a8bf))
* **lint:** apply goimports local-prefix formatting ([8466a00](https://github.com/syscode-labs/imp/commit/8466a00f6593ad6f195797496bb34600d19ee71f))
