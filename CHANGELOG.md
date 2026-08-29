# Changelog

## [0.9.0](https://github.com/syscode-labs/imp/compare/v0.8.0...v0.9.0) (2026-08-29)


### Features

* add microvm expireAfter with pool/template chain ([2707f16](https://github.com/syscode-labs/imp/commit/2707f169f44eb86b2d337f483d05deeb82127191))
* **agent/network:** in-memory IP allocator ([8271b49](https://github.com/syscode-labs/imp/commit/8271b49c39a69881f1d48929a15961388f9d4981))
* **agent/network:** LinuxNetManager — bridge, TAP, NAT (nftables/iptables) ([87a8275](https://github.com/syscode-labs/imp/commit/87a82753170fcb95004859de2b8a676847e820d1))
* **agent/network:** NetManager interface, NetworkInfo, StubNetManager, name helpers ([cdb1e8c](https://github.com/syscode-labs/imp/commit/cdb1e8c91f24d2b430946105a650e2000d4e0996))
* **agent:** add FirecrackerDriver skeleton + go-sdk dependency ([cc77f36](https://github.com/syscode-labs/imp/commit/cc77f362bccc80acc7b515448f590ac70e820d3c))
* **agent:** add HTTP API server skeleton on :9091 ([1fc8fc0](https://github.com/syscode-labs/imp/commit/1fc8fc01c4d5c12676f89ec33dfb9e03d543d65a))
* **agent:** add IsAlive and Reattach to VMDriver interface + StubDriver ([3592983](https://github.com/syscode-labs/imp/commit/3592983115657128f7735a66be582be11a5f6351))
* **agent:** add Metrics+NodeName to FirecrackerDriver, share VMMetricsCollector ([cb99901](https://github.com/syscode-labs/imp/commit/cb99901c64e73772c0d4f6f0cf43d485f341e2f5))
* **agent:** add Snapshot to VMDriver interface, stub implementation ([e430a93](https://github.com/syscode-labs/imp/commit/e430a93b8ea146094b3d2923c02e43b0fa3f3f50))
* **agent:** attach VXLAN interface to bridge on EnsureVXLAN ([4735a90](https://github.com/syscode-labs/imp/commit/4735a90a23ec94135b3714196299095ecd1e555e))
* **agent:** detect host CPU model and patch ClusterImpNodeProfile at startup ([7ec94f1](https://github.com/syscode-labs/imp/commit/7ec94f13d44d4e72e80eef442ed82828419c3bd8))
* **agent:** embed guest-agent binary in agent container image ([90f2ba5](https://github.com/syscode-labs/imp/commit/90f2ba5b33762c0a65323d32c87d9d1acc1a35ca))
* **agent:** envtest suite — mirrors controller suite pattern ([07e942b](https://github.com/syscode-labs/imp/commit/07e942b1bd60084cfe4e1b5b71655eef7474de83))
* **agent:** expose imp_vm_guest_cpu_iowait_ratio OTel gauge ([4584052](https://github.com/syscode-labs/imp/commit/45840523b2a785e71baf0adc4fddcf402c10b194))
* **agent:** finish vxlan sync path, add OTEL traces, and cilium IPAM delegation ([2300cc2](https://github.com/syscode-labs/imp/commit/2300cc23719a5ac306805e441ded68bf1e02ddbc))
* **agent:** FirecrackerDriver.Inspect — kill(pid,0) liveness check ([9fda1be](https://github.com/syscode-labs/imp/commit/9fda1be72c2bec9c782cbf26a9ada225c1e50b33))
* **agent:** FirecrackerDriver.Snapshot — pause/CreateSnapshot/defer-resume ([99bdad8](https://github.com/syscode-labs/imp/commit/99bdad8a871fbb9db55ecb6ba22530a9762e715c))
* **agent:** FirecrackerDriver.Start — class fetch + rootfs build + Firecracker boot ([2bb6000](https://github.com/syscode-labs/imp/commit/2bb6000fafb6a3f6207c84093654f24ce8bce515))
* **agent:** FirecrackerDriver.Stop — graceful ACPI + force kill + socket cleanup ([b723d5e](https://github.com/syscode-labs/imp/commit/b723d5e6de8a7fe756b83525ebdf830abc79fc84))
* **agent:** grant impnetworks list/watch/status-patch RBAC and add NODE_IP env ([3ae79b0](https://github.com/syscode-labs/imp/commit/3ae79b0161d5112035c82db4c6975731ef30cdad))
* **agent:** implement /v1/exec endpoint ([a9601d6](https://github.com/syscode-labs/imp/commit/a9601d6cd4acbffaeaac8030ddbfcbf89bdf0a3a))
* **agent:** implement /v1/serial endpoint ([c04d810](https://github.com/syscode-labs/imp/commit/c04d810aec63bbfc768e7054745ebe16c5922805))
* **agent:** ImpVMReconciler — full state machine, all 4 tests passing ([27294c9](https://github.com/syscode-labs/imp/commit/27294c9dfd7789b14ce19a46b2d09715f560b81d))
* **agent:** ImpVMSnapshotReconciler — node-local and OCI-registry execution ([45f4f41](https://github.com/syscode-labs/imp/commit/45f4f4147ed9ec5eaafb048f416631e3b4989055))
* **agent:** lazy reattach on restart — restore procs + IP allocation + VTEP ([a3be4ad](https://github.com/syscode-labs/imp/commit/a3be4ade179cc7ff2c67e0a1cebaf66429c7b6d1))
* **agent:** migrate metrics to OpenTelemetry SDK ([8b6539b](https://github.com/syscode-labs/imp/commit/8b6539b47b284241488ef0ce2ddd8dcbd137b646))
* **agent:** NAT teardown on last VM stop via Allocator ref-count ([26ab4f2](https://github.com/syscode-labs/imp/commit/26ab4f295e540202e9209b5519f88ceb457c88a4))
* **agent:** OCI push helper — two-layer snapshot image via go-containerregistry ([6951325](https://github.com/syscode-labs/imp/commit/6951325090e4251c81cafaed6ea18c8f85dfa2a0))
* **agent:** poll guest metrics via VSOCK and expose as Prometheus gauges ([b92e3ea](https://github.com/syscode-labs/imp/commit/b92e3eabd5cb2d1e1e759560e7a0fceb5e4d743a))
* **agent:** scale-to-zero wake-on-traffic datapath (Phase 3) ([#29](https://github.com/syscode-labs/imp/issues/29)) ([edb6352](https://github.com/syscode-labs/imp/commit/edb6352638387e2ec1f71fc385070d98b20e5557))
* **agent:** short-circuit Cilium IPAM resolution when Cidr override is set ([1405000](https://github.com/syscode-labs/imp/commit/1405000aebeaadb85cb3f42bd486e7fd6fe9c052))
* **agent:** snapshot-based VM boot via cfg.Snapshot (node-local) ([f1add8b](https://github.com/syscode-labs/imp/commit/f1add8b53cd129a4a946931bbdaec23b1f65ed3e))
* **agent:** StartedAt timeout for stuck Starting VMs; hostPath socketDir default ([b9b6d38](https://github.com/syscode-labs/imp/commit/b9b6d388ada10d310df3dbddac11898bc9f7a8d4))
* **agent:** StubDriver — thread-safe fake VMDriver for testing ([baf0be6](https://github.com/syscode-labs/imp/commit/baf0be6ff18e382dc6c4463fcf59c1e83423dd8b))
* **agent:** use group CIDR from network status when VM has spec.networkGroup set ([14a9201](https://github.com/syscode-labs/imp/commit/14a92010c724919c70df5a095f92a1476103e464))
* **agent:** VMDriver interface and VMState type ([c07c67a](https://github.com/syscode-labs/imp/commit/c07c67acfe3d4bda12b0d53e106a01f6ce5494c2))
* **agent:** wire FirecrackerDriver into main.go — reads FC_KERNEL/FC_BIN/FC_SOCK_DIR ([2874008](https://github.com/syscode-labs/imp/commit/28740089637e24a6243b345664d9c020eaa715d9))
* **agent:** wire guest agent injection + probe runner into FirecrackerDriver ([11e2ce1](https://github.com/syscode-labs/imp/commit/11e2ce151a5a6fcd3c02b5d9f0c33f58654e77d5))
* **agent:** wire ImpVMReconciler + StubDriver into controller-runtime manager ([5926a96](https://github.com/syscode-labs/imp/commit/5926a96885ad0878f151f7db9fc61b5671fc0541))
* **agent:** wire ImpVMSnapshotReconciler into agent manager ([43fafcb](https://github.com/syscode-labs/imp/commit/43fafcb9b58f9885406c8ea9c10c54059944d9e4))
* **agent:** wire LinuxNetManager into production driver ([eafa787](https://github.com/syscode-labs/imp/commit/eafa7873e0201e94f2f631c170aa9eeb9858be09))
* **agent:** wire NetManager + IP allocator into FirecrackerDriver ([1a2424d](https://github.com/syscode-labs/imp/commit/1a2424d71ae61eed93abde46d30ecf8463d7f581))
* **agent:** wire probe condition patcher to ImpVM status patch ([454e952](https://github.com/syscode-labs/imp/commit/454e9525252d751e4c5032a8e82ad70e33eab2a3))
* **agent:** write Firecracker serial console output to log file ([3359806](https://github.com/syscode-labs/imp/commit/335980677289a73b3d78d9ee5d4ebd05f77573e5))
* **allocator:** sizeToCIDRPrefix — isolation-first CIDR sizing ([bf38b17](https://github.com/syscode-labs/imp/commit/bf38b17c08c5ca36bc365a8d834e6e4c802141f7))
* API changes — HTTPCheckSpec, runtimePID, Scheduled/Terminating phases ([e2aafc4](https://github.com/syscode-labs/imp/commit/e2aafc4378e1906eb9833df3dfaa090402f4f1cd))
* **api,network:** add VTEPEntry/VTEPTable types and VXLAN NetManager methods ([fc0b73a](https://github.com/syscode-labs/imp/commit/fc0b73a00df5e12a269eab46c9be155e9904cc48))
* **api,rootfs:** add RunnerLayer/CiliumLayer fields and BuildComposite overlay ([7bf6581](https://github.com/syscode-labs/imp/commit/7bf6581e45ba7cfbf84a6e8bf9b0ad1f75deee5f))
* **api:** add CiliumIPAMSpec.Cidr override field ([f93f83a](https://github.com/syscode-labs/imp/commit/f93f83af1492549b90b1babfffd2de5e01ef1b0b))
* **api:** add GroupCIDR type and ImpNetworkStatus.GroupCIDRs field ([d2958f6](https://github.com/syscode-labs/imp/commit/d2958f6ac0ab6039110eb8d2cab12536f5dbe8f7))
* **api:** add ImpNetworkAttachment CRD with LAN/VLAN allowlist types ([8ea0f3f](https://github.com/syscode-labs/imp/commit/8ea0f3f500da1facd844285f117cc3f0de0adbdc))
* **api:** add ImpVM.spec.snapshotRef for snapshot-based boot ([5bc1a63](https://github.com/syscode-labs/imp/commit/5bc1a63f2b05c210c6433772a0c746729b4072df))
* **api:** add ImpVMSpec.RescheduleOnNodeLoss opt-in field ([d57cfac](https://github.com/syscode-labs/imp/commit/d57cfacda5807d0027b0714a763a1636dfa49c8a))
* **api:** add RestartPolicy types + restart status fields ([7021c7a](https://github.com/syscode-labs/imp/commit/7021c7a1e7baa969816166351e30a732fd6fff78))
* **api:** add ScaleToZero desiredState mode + idleTimeout (Phase 3 foundation) ([#28](https://github.com/syscode-labs/imp/issues/28)) ([20be988](https://github.com/syscode-labs/imp/commit/20be988a464e4a630d6ccfa43c56b18ab8455810))
* **api:** add Tolerations field to ImpVMSpec and ImpVMClassSpec ([61f620b](https://github.com/syscode-labs/imp/commit/61f620ba8df4bfa97a28e643d99d54a5617ac0b5))
* **api:** add VCPUCapacity and MemoryMiB to ClusterImpNodeProfileSpec ([1645d2f](https://github.com/syscode-labs/imp/commit/1645d2fd6030a95299d15b04b436156913f10dbd))
* **api:** GuestAgentConfig field + resolver (ImpVMClass → ImpVM inheritance) ([12cd403](https://github.com/syscode-labs/imp/commit/12cd40383784df12ea69e7e31bd2a36a605e3d4d))
* **api:** ImpVMRunnerPool CRD — CI runner pool with scaling and job detection ([b15344e](https://github.com/syscode-labs/imp/commit/b15344ee1cf2cd9e6ab40db8bfb17ce7aeef7138))
* **api:** ImpVMSnapshot — NodeLocalSpec, BaseSnapshot election, TerminatedAt, retention bounds ([7d7161e](https://github.com/syscode-labs/imp/commit/7d7161e305219b98eb5a5a2f26034f2dca3dfa09))
* **api:** ImpVMSnapshot, ImpWarmPool, ImpVMMigration CRDs + CPUModel field ([89a9a16](https://github.com/syscode-labs/imp/commit/89a9a1626cc79ebd754fc56749df656bf81553bc))
* **api:** IPAMSpec, NetworkGroupSpec, networkGroup field ([1411604](https://github.com/syscode-labs/imp/commit/1411604bb542285395e8fa64c82d40f24c146608))
* **charts:** operator HA — 2 replicas, PDB, required anti-affinity ([29fa1a7](https://github.com/syscode-labs/imp/commit/29fa1a78bfd10d1bc6911eb58727c0e126d77e18))
* **charts:** operator HA (replicas, PDB, anti-affinity) ([24aeebb](https://github.com/syscode-labs/imp/commit/24aeebbd1214889e9b8650f95c372090890ff5b4))
* **cnidetect:** Detect function with CRD + DaemonSet heuristics ([44221c0](https://github.com/syscode-labs/imp/commit/44221c015bb759ec028d763174361cabff0f85a1))
* **cnidetect:** thread-safe Store for CNI detection result ([fb8e7ab](https://github.com/syscode-labs/imp/commit/fb8e7ab45ef4d02feb9bc81ba19fd31ba396723d))
* **controller,agent:** VTEP registration and FDB sync for cross-node VXLAN ([b6f78bf](https://github.com/syscode-labs/imp/commit/b6f78bf43ec08ce91f16a2213e53543b5df81258))
* **controller,api:** Cilium external workload enrollment for network VMs ([d156b87](https://github.com/syscode-labs/imp/commit/d156b879c9fd94155a89596d6d802cdb62f731eb))
* **controller:** add carveGroupCIDRs pure function for network group CIDR allocation ([a4fc2a0](https://github.com/syscode-labs/imp/commit/a4fc2a0f82c600ec5c3a2a3ff7b64059c9339a14))
* **controller:** add CNIDetected/CNIAmbiguous event reason constants ([ed91582](https://github.com/syscode-labs/imp/commit/ed915820f4340dcf8e0b6d60518ba69c3a29e2df))
* **controller:** add ImpNetwork event reason + condition constants ([255a417](https://github.com/syscode-labs/imp/commit/255a4174269626fd2666e32ae0d078dafc23b324))
* **controller:** add pure Schedule() function with resource-fit and debug logging ([895f7b6](https://github.com/syscode-labs/imp/commit/895f7b6261f535d167ea1954e4cf6f9ac51d8f4c))
* **controller:** auto-create CiliumPodIPPool as owned child of ImpNetwork ([3b1848a](https://github.com/syscode-labs/imp/commit/3b1848a31e967d579e682fd3b86d11eab6f47e97))
* **controller:** capacity-aware scheduling using node allocatable CPU/memory ([54ab4e9](https://github.com/syscode-labs/imp/commit/54ab4e937c6d5614ff90592203bf8a7ff7c80855))
* **controller:** effectiveMaxVMs + parseFraction capacity helpers ([6cf2ad6](https://github.com/syscode-labs/imp/commit/6cf2ad6ca2248487f8b4e5efe1fab3d654c07444))
* **controller:** filter unready and unschedulable nodes in scheduler ([87e60e9](https://github.com/syscode-labs/imp/commit/87e60e995675f300dcafcd98b01316773bb5e360))
* **controller:** ImpNetworkReconciler — finalizer, Ready condition, CNIDetected event ([94a64ee](https://github.com/syscode-labs/imp/commit/94a64eeef7d3acbb769cb1b90f96a1d202232cef))
* **controller:** ImpVMMigration full state machine — snapshot → restore → delete source ([0d8aa82](https://github.com/syscode-labs/imp/commit/0d8aa8209a35c4bfe7f330704e75ce7a44abff17))
* **controller:** ImpVMMigration skeleton — CPU-compatible node selection, node drain watcher ([a21dfc9](https://github.com/syscode-labs/imp/commit/a21dfc91c2ec5bf53d957bde91989275f10060dc))
* **controller:** ImpVMRunnerPoolReconciler — ephemeral CI runner VM pool ([7fea1e9](https://github.com/syscode-labs/imp/commit/7fea1e90be412201a96798be04e5e8f32b87ad22))
* **controller:** ImpVMSnapshot operator — child creation, retention, BaseSnapshot validation, cron ([9b9464c](https://github.com/syscode-labs/imp/commit/9b9464c3ff57ee1b2751b25edba5e498aebe76a2))
* **controller:** ImpVMSnapshot reconciler skeleton ([54ee683](https://github.com/syscode-labs/imp/commit/54ee6836aa676b4f47d04ef7fbd875ef621c085d))
* **controller:** ImpWarmPool — maintain pool of snapshot-booted VMs ([36594ed](https://github.com/syscode-labs/imp/commit/36594ed179cbd6479a5317afc78995540c87524f))
* **controller:** manual-reset annotation + cool-down auto-reset ([f7702f8](https://github.com/syscode-labs/imp/commit/f7702f84509f7f2cb00376bee8f77bdefc051d5c))
* **controller:** migrate metrics to OpenTelemetry SDK ([d0a6c88](https://github.com/syscode-labs/imp/commit/d0a6c88f2f20121f03da3f228f3ddf9f5a232158))
* **controller:** reconcile group CIDRs into ImpNetwork status ([8a071c7](https://github.com/syscode-labs/imp/commit/8a071c766c8bb8d422a8210f181138e4e47d9ce7))
* **controller:** reschedule persistent VM on node loss when RescheduleOnNodeLoss=true and no PVC ([6226982](https://github.com/syscode-labs/imp/commit/622698246325ab80a9b08508107a16140d0d880b))
* **controller:** resolveClassSpec — follows ClassRef or TemplateRef chain ([de41235](https://github.com/syscode-labs/imp/commit/de412352715b4a3ca00403faaa3a0900ca8768de))
* **controller:** resource-aware scheduling via explicit VCPUCapacity/MemoryMiB ([36e989d](https://github.com/syscode-labs/imp/commit/36e989d46a8bc58dbf61ac47abc480c4d6dbafd2))
* **controller:** restart backoff — exponential delay, in-place/reschedule modes ([81ffde8](https://github.com/syscode-labs/imp/commit/81ffde868a59ea21037cd69ef97e56c96d1ad686))
* **controller:** scale runner pools from webhook demand annotation ([044bd34](https://github.com/syscode-labs/imp/commit/044bd349fd44f0c34eeacbab7be9091c0f105cb5))
* **controller:** taint/toleration scheduling filter with class merge ([ef75fe7](https://github.com/syscode-labs/imp/commit/ef75fe7a078721c62668b96f25b79bd670d8559b))
* **crd:** add cidr field to CiliumIPAMSpec in generated CRD manifest ([89feb5a](https://github.com/syscode-labs/imp/commit/89feb5af14770383d689089caf7baf5a21172b82))
* **e2e:** Layer 1 Kind-based E2E — CRDs, operator health, webhooks, CRUD, metrics ([d7c4b74](https://github.com/syscode-labs/imp/commit/d7c4b74a20b08e5de86fe88f445fe5e82f36236f))
* enforce 60s minimum for expireAfter ([4d08136](https://github.com/syscode-labs/imp/commit/4d08136edd2b08bc9f7846e7eb87ec6b9aa66f82))
* event constants and condition helpers ([c5ca717](https://github.com/syscode-labs/imp/commit/c5ca7170a3b53339585a849a8c4de1da48d3c862))
* **examples:** add tiny smoke assets and validation ([37667bd](https://github.com/syscode-labs/imp/commit/37667bd9ec951cdb7dca0cdcde8e456d91758ed7))
* **guest:** add cpu_iowait_ratio to Metrics RPC ([b24b7c1](https://github.com/syscode-labs/imp/commit/b24b7c1552634dcbf40fe98b3f64d839f66cec9e))
* **guest:** gRPC server — Exec, HTTPCheck, Metrics ([fec78e7](https://github.com/syscode-labs/imp/commit/fec78e70474304ab83be1346583fa4fe95fd1192))
* **hack:** add Object Storage overlay to avoid ~GBP 0.90/month custom image cost ([1b038a4](https://github.com/syscode-labs/imp/commit/1b038a40efb6ecd19586e6bdc1da374375c664e6))
* **hack:** add OCI Firecracker end-to-end standalone smoke runner ([660cf31](https://github.com/syscode-labs/imp/commit/660cf31fda6a7825b8f4e489edaade29bca90702))
* **hack:** OCI golden image builder, Packer wrapper, and e2e improvements ([8885048](https://github.com/syscode-labs/imp/commit/8885048fe62edfd53d08af451c3f952acf668960))
* **helm:** add Grafana dashboard ConfigMap with fleet, latency, and guest metrics panels ([25ac43a](https://github.com/syscode-labs/imp/commit/25ac43a644ecf75eb80017608efb6643cd9bfe13))
* **helm:** add plugin ClusterRoles for readonly and admin access ([172fecb](https://github.com/syscode-labs/imp/commit/172fecba9ce0fefa6dc0b6da72acd8382b651216))
* **helm:** add traces dashboard and OTEL_SERVICE_NAME to agent and operator ([9c981a6](https://github.com/syscode-labs/imp/commit/9c981a6a4a2f9a1c551af1de2432083977f539af))
* **helm:** agent DaemonSet — /dev/kvm, optional hostPaths, NODE_NAME downward API ([e5279fc](https://github.com/syscode-labs/imp/commit/e5279fc544442263029c6112e34af9bbad49957c))
* **helm:** agent RBAC — ServiceAccount, ClusterRole, ClusterRoleBinding ([ddd6bf0](https://github.com/syscode-labs/imp/commit/ddd6bf05915cab16fefa9a6cd83e71a9ac7cf7f5))
* **helm:** imp chart scaffold — Chart.yaml, values.yaml, _helpers.tpl ([e3fa123](https://github.com/syscode-labs/imp/commit/e3fa1238a0f0f5f6a359489d591488467b51dbaf))
* **helm:** imp-crds chart — six CRDs with resource-policy keep ([a1aa3e3](https://github.com/syscode-labs/imp/commit/a1aa3e3bc2b7bdc30c352c416ffb8e55a8ea3e05))
* **helm:** operator Deployment and Service ([8df9b09](https://github.com/syscode-labs/imp/commit/8df9b09e0c3c2c62d5536dde6b1666cb97fc681f))
* **helm:** operator RBAC — ServiceAccount, ClusterRole, leader-election Role ([6d2739a](https://github.com/syscode-labs/imp/commit/6d2739a989f43739245e7e0704b40438c6b11858))
* **helm:** ServiceMonitor + PodMonitor for Prometheus Operator (default enabled) ([015267f](https://github.com/syscode-labs/imp/commit/015267f51a5f6169ea973a0f709bc57f75212b27))
* **helm:** webhook resources — cert-manager Certificate, Issuer, webhook configs ([841b7aa](https://github.com/syscode-labs/imp/commit/841b7aad1687489ad7a8998f70307267bda6a188))
* **metrics:** node agent /metrics on :9090 — imp_vm_state, guest CPU/mem/disk ([b20ed49](https://github.com/syscode-labs/imp/commit/b20ed49ea335700b971dfe82e8613c47f749e5c0))
* **metrics:** operator scheduling + boot latency histograms, timestamps in ImpVM status ([24dcea9](https://github.com/syscode-labs/imp/commit/24dcea94f49f3e8b7806a8c1879b20fa455de817))
* **network:** add RBAC-gated LAN/VLAN attachment for microVMs ([035fa58](https://github.com/syscode-labs/imp/commit/035fa580da98f03029373702d4a60a416178aac5))
* **network:** add RemoveNAT to NetManager interface and LinuxNetManager ([56e9e57](https://github.com/syscode-labs/imp/commit/56e9e57ecf8e1c77f7241590489465af03b0dbfa))
* **network:** Allocator.Release returns wasLast bool ([fe3f70d](https://github.com/syscode-labs/imp/commit/fe3f70daca67099d5e0dbc72c089f90a199ceb25))
* **network:** gate LAN/VLAN attachment behind admission and node bindings ([42745c6](https://github.com/syscode-labs/imp/commit/42745c63fb77018d624ca7bbb13268f2ebe8499a))
* Node watch for reactive reconcile; rename go-scaffold → imp labels ([0f73186](https://github.com/syscode-labs/imp/commit/0f73186ffccc4f315cafe2597cf3ca7d1512f549))
* **operator:** log Cilium CRD presence at startup ([2bbeded](https://github.com/syscode-labs/imp/commit/2bbeded3ff8aa82cc6d88923af238e770cbd1256))
* **operator:** register ImpNetworkReconciler ([19bd552](https://github.com/syscode-labs/imp/commit/19bd5526fd3bc8f4c14296e20514a7cc0111cf9f))
* **operator:** register ImpVM/ImpVMClass/ImpVMTemplate admission webhooks ([17341cf](https://github.com/syscode-labs/imp/commit/17341cf813302b46966f04502d84c7417147b340))
* **operator:** run CNI detection at startup, emit CNIDetected event ([adff4eb](https://github.com/syscode-labs/imp/commit/adff4eb3d0ebd9167a08c03f14224a9ab8540d66))
* Phase 1 completion — VSOCK guest agent, probes, Prometheus metrics, Helm monitors, Kind E2E ([7047cc1](https://github.com/syscode-labs/imp/commit/7047cc15dc3cbf632c838ad420d5d4b0e6b3f07b))
* Phase 2 — suspend-on-idle for ImpVM ([#24](https://github.com/syscode-labs/imp/issues/24)) ([b9fc5c5](https://github.com/syscode-labs/imp/commit/b9fc5c586913c2b3bc2256f1f9799eec19ed9bcd))
* **probe:** probe runner — polls gRPC exec/http probes, patches conditions ([26547c8](https://github.com/syscode-labs/imp/commit/26547c888443c8e931fb1b1b9e1d2661eeea00d3))
* **proto:** gRPC guest agent service definition ([13cef85](https://github.com/syscode-labs/imp/commit/13cef8585d4e7e2ffa3484a4dbe1101e58a15d98))
* **rbac:** grant operator CiliumPodIPPool CRUD ([9df428a](https://github.com/syscode-labs/imp/commit/9df428a67d4ec209e2b2a10c794deb5452dd0c6b))
* restore scaling-only runner pool design and examples ([861fa84](https://github.com/syscode-labs/imp/commit/861fa84e7130af8b453f43d11abdb66acad299e2))
* **rootfs:** add Builder skeleton + test helpers ([68f5e77](https://github.com/syscode-labs/imp/commit/68f5e774fbf41bd7d3da34bee29a51e245196053))
* **rootfs:** Build() — full OCI→ext4 pipeline with digest-based cache ([07095f2](https://github.com/syscode-labs/imp/commit/07095f2f96415ae686c4ff2b51faec93c452aa61))
* **rootfs:** buildExt4 — assemble ext4 via mke2fs -d ([482aa9c](https://github.com/syscode-labs/imp/commit/482aa9c9911fac07bd6a4b51c8b72f1e3140b8a2))
* **rootfs:** extractLayers — squash OCI layers into temp dir via archive/tar ([238da39](https://github.com/syscode-labs/imp/commit/238da39de52873cc40ad805f293d5dc039b4f88c))
* **rootfs:** guest agent injection + WithGuestAgent BuildOption ([165859f](https://github.com/syscode-labs/imp/commit/165859f64fffd2ec8e2f14a5edd911942dee7043))
* **rootfs:** pullImage — fetch manifest via go-containerregistry ([f5950fc](https://github.com/syscode-labs/imp/commit/f5950fc86054a8b56606ec7717fd4f56e6b6c5e3))
* **rootfs:** writeInit — write /sbin/init from OCI CMD/ENTRYPOINT ([e798d83](https://github.com/syscode-labs/imp/commit/e798d834ccc0a830dcc5b5b2c94878ea70e43094))
* **runner:** PlatformDriver interface — GitHub Actions, GitLab, Forgejo ([d97fcc4](https://github.com/syscode-labs/imp/commit/d97fcc40599ec39b899c7f09a1648383953ef5c6))
* **runtime:** separate node VM ownership ([d4c2421](https://github.com/syscode-labs/imp/commit/d4c2421d641f54e32c91a0e0580da9b4ed900fcb))
* **sandbox:** guest file API, gateway data plane, LAN-attachment guard ([#63](https://github.com/syscode-labs/imp/issues/63)) ([dbfa4c6](https://github.com/syscode-labs/imp/commit/dbfa4c60d65439a44b8063bdc23ff575675df4d1))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([53259a8](https://github.com/syscode-labs/imp/commit/53259a8b553a3e360fd2984e2a14ce308ab65c91))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([b2087aa](https://github.com/syscode-labs/imp/commit/b2087aa22dc4c1242efd9eb3823410ab15c889dc))
* **sandbox:** sandbox add-on (rebase + e2e harness fix) ([a0ec6a6](https://github.com/syscode-labs/imp/commit/a0ec6a6843909848931fc9d030a8522e771869cf))
* **scheduler:** overcommit suspended VMs' freed capacity (Phase 2 Task 4) ([#25](https://github.com/syscode-labs/imp/issues/25)) ([086a928](https://github.com/syscode-labs/imp/commit/086a928966d6445010995ef25f71309e89798a5e))
* **scheduling:** memory-pressure guardrails ([729a3df](https://github.com/syscode-labs/imp/commit/729a3dffbeabd8186c1a185748d15157a0b87a7a))
* **scheduling:** memory-pressure guardrails — QoS/priority classes, scheduling reserve, opt-in pressure lifecycle ([185e492](https://github.com/syscode-labs/imp/commit/185e4925bad017cfef30e8cefc7a3fa230347f8e))
* **telemetry:** add SetupMeterProvider with Prometheus + optional OTLP ([4902bcf](https://github.com/syscode-labs/imp/commit/4902bcf4089a4e46d704816d982b9041fc0eff7e))
* **tracing:** add agent reattach, vtep_register, fdb_sync, and snapshot spans ([76b0e44](https://github.com/syscode-labs/imp/commit/76b0e44a5bc5866d4e08a7d9b801e2fda4fef1b0))
* **tracing:** add agent.impvm.reconcile, start, and stop spans ([86f2063](https://github.com/syscode-labs/imp/commit/86f2063b4cad5238e65153ca1302b744616b7d8a))
* **tracing:** add agent.impvm.rootfs_build and firecracker_launch spans ([a93159d](https://github.com/syscode-labs/imp/commit/a93159dfb0c7423ad98b8a2786f3c4be80a713cf))
* **tracing:** add operator.impnetwork reconciler spans ([b6b6809](https://github.com/syscode-labs/imp/commit/b6b6809c64f7673f3bc53784a9825e4aeae9a319))
* **tracing:** add operator.impvm.reconcile and operator.impvm.schedule spans ([96eb969](https://github.com/syscode-labs/imp/commit/96eb9690bca0e71f5434dbb1b77ab00c74d1c5dc))
* **tracing:** add operator.impvmmigration reconciler spans ([fa1cee3](https://github.com/syscode-labs/imp/commit/fa1cee3046aa93d51bc611ca334cc938dea3c803))
* **tracing:** add operator.impvmsnapshot reconciler spans ([bb4b930](https://github.com/syscode-labs/imp/commit/bb4b9303d53d4a3965dcfc1502b948e6394c8a24))
* **tracing:** add SpanFromVM, InjectToVM, RecordError helpers ([5e045b9](https://github.com/syscode-labs/imp/commit/5e045b918565c6197cd421407ec5e8f50bc5c857))
* **tracing:** register W3C TraceContext propagator in agent and operator ([3d1a08e](https://github.com/syscode-labs/imp/commit/3d1a08ed8168a401f4f839944ef510feac816a1b))
* **vsock:** host-side gRPC dialer over Firecracker VSOCK proxy ([f1d1ed6](https://github.com/syscode-labs/imp/commit/f1d1ed63fd584a2739fce940e7fef204b694ef4c))
* **webhook:** ImpVM defaulter + validator ([2c3972b](https://github.com/syscode-labs/imp/commit/2c3972bdd33e452d40c785e9124218f27ee04631))
* **webhook:** ImpVMClass validator stub ([fae01f6](https://github.com/syscode-labs/imp/commit/fae01f66c42b3b64434b3bbcf5820b37e4df036c))
* **webhook:** ImpVMTemplate validator ([36901ee](https://github.com/syscode-labs/imp/commit/36901ee672e50ff51ce6fcaa0d3bceac064d2d26))
* **webhook:** inherit restartPolicy through class→template→VM chain ([632c281](https://github.com/syscode-labs/imp/commit/632c2812e84e455168ecb41ced7b01ba36a0e52c))


### Bug Fixes

* address code review issues ([9d1495a](https://github.com/syscode-labs/imp/commit/9d1495afafc3da8a2dbb967eeeb6d19cfb9775dd))
* **agent,controller:** take DeepCopy base before slice filter in VTEP helpers ([29c6bdc](https://github.com/syscode-labs/imp/commit/29c6bdc3a6c5dab35e58d199bfb6698c860ce877))
* **agent,metrics:** insert proc before goroutine launch; add ResetMetricsForTest stub ([6f07f39](https://github.com/syscode-labs/imp/commit/6f07f39acf3d29b741ff99c486a2e64a90497008))
* **agent/network:** correct BridgeName length bound in test, add TAPName empty guard ([9819d37](https://github.com/syscode-labs/imp/commit/9819d37a8a4e8d68ffeb3b68ff5de78de8a67278))
* **agent/network:** make EnsureNetwork/SetupVM idempotent, add nft duplicate guard ([3ca173f](https://github.com/syscode-labs/imp/commit/3ca173f25ba2b9cd7ba215ae7399e6cbfc90585a))
* **agent:** cleanup stale vsock sockets on stop/start paths ([7817371](https://github.com/syscode-labs/imp/commit/7817371bbc646d3be8545bdc3be57d82769a1987))
* **agent:** clear StartedAt in finishFailed to prevent immediate re-timeout on restart ([65a169f](https://github.com/syscode-labs/imp/commit/65a169fa793cb620c51dfd50c33b91e135b1360d))
* **agent:** correct Go toolchain version, remove -a flag, share build cache id ([13852f2](https://github.com/syscode-labs/imp/commit/13852f21c9325a0978a45512705795771324154e))
* **agent:** correct nolint directive, add comment on metrics test coverage ([70e127e](https://github.com/syscode-labs/imp/commit/70e127ef4c323418e1e04e9bf1676b073f98ab14))
* **agent:** correct serial log comment and add shutdown timeout to API server ([50137b5](https://github.com/syscode-labs/imp/commit/50137b598fe65a9a0399a8d9efe5c535531ead56))
* **agent:** deliver firecracker binary via hostPath, wire FC_BIN + KVM preflight ([c3ba72d](https://github.com/syscode-labs/imp/commit/c3ba72d49b417059f7f8bce2b9c9e8c4bf60f455)), closes [#33](https://github.com/syscode-labs/imp/issues/33)
* **agent:** fix AF_PACKET wake hook so scale-to-zero actually wakes ([#35](https://github.com/syscode-labs/imp/issues/35)) ([a0ccf8c](https://github.com/syscode-labs/imp/commit/a0ccf8c83c294b2189e043b51ec41fc6eb624456))
* **agent:** nil guard for d.Client in probe patcher closure ([3bd36b1](https://github.com/syscode-labs/imp/commit/3bd36b1a30de494979cb14c0a3c70ccbdf270d5a))
* **agent:** probe goroutine lifetime, data race, and silent error ([a4fa5b4](https://github.com/syscode-labs/imp/commit/a4fa5b4df6e84deac3ff4c70afd2b8acd835654b))
* **agent:** propagate vm env into init and guest-agent ([9c44325](https://github.com/syscode-labs/imp/commit/9c44325eadca1e7037c3b9539a0c69607eb08793))
* **agent:** reduce reconcile churn with configurable retry interval ([5059cf4](https://github.com/syscode-labs/imp/commit/5059cf42228ac077e617becbcf5c8a39e5ef9739))
* **agent:** resolve VTEP IP via direct client at startup ([906bf04](https://github.com/syscode-labs/imp/commit/906bf04c7fc5ed10c9067293206af2aee6feea72))
* **agent:** run CPU-model patch after cache sync ([4ebb4cf](https://github.com/syscode-labs/imp/commit/4ebb4cfb04c5f5e32cbf3704da3d797f28c0a3d8))
* **agent:** scope wasLast correctly for egress-deny cleanup ([b96f8bc](https://github.com/syscode-labs/imp/commit/b96f8bc8f5ddb3687f885232d9e266f1222971ae))
* **agent:** set TerminatedAt=Failed when source VM not found in snapshot reconciler ([695eef3](https://github.com/syscode-labs/imp/commit/695eef3a3bc440cb9f24e8d6cfd29a80b2a49940))
* **agent:** socket cleanup on Start error + key var in Inspect + stale comment ([0585175](https://github.com/syscode-labs/imp/commit/0585175a904825f65d017bc8931feb720fd298f7))
* **agent:** use per-stage BuildKit cache IDs to avoid parallel write conflict ([8f8cb10](https://github.com/syscode-labs/imp/commit/8f8cb1067d571f49ab7f60f6d86f418f6566146c))
* **agent:** VTEPTable +listType=map + optimistic retry in registerVTEP ([a5e2c45](https://github.com/syscode-labs/imp/commit/a5e2c450dfdfe91e8d97dcd5a274fc9310d1ae0b))
* **agent:** wrap LinkSetMaster error and extend VXLAN stub tests ([d86195a](https://github.com/syscode-labs/imp/commit/d86195ae0300f58e2284278553c6d832ab68b1fe))
* **api:** add conditions to ImpVMMigrationStatus ([02a726c](https://github.com/syscode-labs/imp/commit/02a726cf7fcff451e30fd7ec754d295ae772cac2))
* **api:** go mod tidy, duration pattern validation, test coverage ([d8ebf2d](https://github.com/syscode-labs/imp/commit/d8ebf2d148fc9235f187bf36802793bc26fb181d))
* **api:** ImpVMRunnerPool — optional markers, MaxConcurrent cap, scope XValidation ([0aa5aa4](https://github.com/syscode-labs/imp/commit/0aa5aa4316fa91462234e8a89948d2fa9b2b9ecd))
* **api:** regenerate deepcopy and sync Helm CRDs for Tolerations fields ([3762cb7](https://github.com/syscode-labs/imp/commit/3762cb7b93df03d401353e4ef7f56d27e30d3da3))
* **api:** regenerate deepcopy for GuestAgentConfig pointer fields ([311c4ac](https://github.com/syscode-labs/imp/commit/311c4ac509fa92ba6e1b33c397a480d608c3f41d))
* **api:** use pointer types for optional struct fields in ImpVMRunnerPool ([49cf17d](https://github.com/syscode-labs/imp/commit/49cf17d010499e183819d906092c9dd86e6c80e8))
* **chart:** default agent root security context and daemonset rbac ([b17743f](https://github.com/syscode-labs/imp/commit/b17743f4bc6b2d6f1902f46b5eab4315abe14e8c))
* **charts,e2e:** enable local Kind e2e and runner-pool demand validation ([ce24cc8](https://github.com/syscode-labs/imp/commit/ce24cc84e2a716e2b9eb6069c877961c9f27a24b))
* **charts:** grant agent read on impnetworkattachments ([a3051ae](https://github.com/syscode-labs/imp/commit/a3051ae895982cd09d1049c716a00685774d5cfb))
* **charts:** grant imp-runtime read on impnetworks ([8e97d81](https://github.com/syscode-labs/imp/commit/8e97d81c7fc76326b841a7a0801e267e4f08ce36))
* **charts:** resync drifted CRDs from base + add make sync-chart-crds ([#31](https://github.com/syscode-labs/imp/issues/31)) ([c905d33](https://github.com/syscode-labs/imp/commit/c905d33125c31246fc6d63d5ce1119fe8cd5f47a))
* **charts:** support digest-pinned Imp images ([a1d5842](https://github.com/syscode-labs/imp/commit/a1d5842eef45caa39a534beaea97a3446948fb33))
* **charts:** support Talos extension host paths ([77b641f](https://github.com/syscode-labs/imp/commit/77b641f41931e1f6ed319bd384c3fd431cf5b584))
* **charts:** support Talos host paths and digest pins ([f0c1170](https://github.com/syscode-labs/imp/commit/f0c11704a101aaf97ed6ce61f0298942548674a5))
* **charts:** sync CRD bundle and operator RBAC with implemented controllers ([95c4638](https://github.com/syscode-labs/imp/commit/95c463886a336365542c91d75db09caf2d83d456))
* **ci:** address lint deprecation and retry interval tests ([b89e84f](https://github.com/syscode-labs/imp/commit/b89e84fe33da8bfa86c7e2f0e4870f4430b2cdb6))
* **ci:** publish amd64 runtime images ([#51](https://github.com/syscode-labs/imp/issues/51)) ([e75941d](https://github.com/syscode-labs/imp/commit/e75941d5dd648f7e5056e120d59934b47e387ee2))
* **ci:** resolve lint and static analysis blockers ([dc993fd](https://github.com/syscode-labs/imp/commit/dc993fd5917138fcbc7194a2e420ee5bb100caca))
* **ci:** stabilize lint and nightly kind smoke workflows ([1b17e4f](https://github.com/syscode-labs/imp/commit/1b17e4fea2f23179033505ae59dc2468e4747814))
* **ci:** stabilize linux lint and tests ([4502e50](https://github.com/syscode-labs/imp/commit/4502e50fb4d9301ac4ebc40c2f7d018d316b7535))
* **ci:** stabilize scaling.mode CEL validation rule ([8ea8f23](https://github.com/syscode-labs/imp/commit/8ea8f235cdca2dc6e5d7c7da857a72850797a8bf))
* **ci:** use legacy recorder API with staticcheck waiver ([fc25e7c](https://github.com/syscode-labs/imp/commit/fc25e7cb3dcebadc6b3136dc946632c79e72bd76))
* **cnidetect:** stop spawning cached DaemonSet informer ([e848b67](https://github.com/syscode-labs/imp/commit/e848b67e74045f82ea8d28d6e0ded162475b2138))
* **controller,agent:** restart flow, runner demand scaling, and composite layer wiring ([1f6abed](https://github.com/syscode-labs/imp/commit/1f6abeda31245cc70ca7e889ff23702f8e1c4e8d))
* **controller:** correct baseSnapshot prune exemption and validation gate ([e9b5c8e](https://github.com/syscode-labs/imp/commit/e9b5c8e4e2afecb7c000649a241e23b4ff579ff1))
* **controller:** delete CiliumPodIPPool on ImpNetwork deletion and strengthen noop test ([ccfb574](https://github.com/syscode-labs/imp/commit/ccfb5747d98c449601284b0880b6858d7f7fc0c6))
* **controller:** extract effectiveMaxRetries, fix event message, add TODO comments ([f95a768](https://github.com/syscode-labs/imp/commit/f95a768974fdff0143f5e8d09048adb629b797fa))
* **controller:** fail fast on invalid ImpVM reference wiring ([c258c24](https://github.com/syscode-labs/imp/commit/c258c244bd78517d31d046c8df24350c1e5b4d9d))
* **controller:** guard empty SnapshotRef, requeue unscheduled source VM ([c24a8cc](https://github.com/syscode-labs/imp/commit/c24a8cc3da54cdc924528ac86804f254e77ab0ae))
* **controller:** guard handleResetRetries against non-terminal VM phases ([3420695](https://github.com/syscode-labs/imp/commit/3420695775719f781bfdf4f542e10d1b4963de93))
* **controller:** guard toCreate against negative, remove dead vmName variable ([85869d2](https://github.com/syscode-labs/imp/commit/85869d2962b13ecf07aca3ab5400b018fec0adc9))
* **controller:** ImpVMRunnerPoolReconciler — idle count, DeletionTimestamp guard, ClassRef copy ([c1bec33](https://github.com/syscode-labs/imp/commit/c1bec337182cce0da379f9869149cfbcaf88d61f))
* **controller:** namespace-qualify CEW names and fix existingCEWNames ordering ([e774235](https://github.com/syscode-labs/imp/commit/e7742354616dbe326fa347ca42871788506ec5b8))
* **controller:** stale child list, second-granular names, Owns watch, clock rebase ([9305ffb](https://github.com/syscode-labs/imp/commit/9305ffb11a0557d8b38a9d323db52134265747d4))
* correct RBAC order for leases (match controller-gen output) ([0b0f9c6](https://github.com/syscode-labs/imp/commit/0b0f9c67c0e064f11eabbf74e363fb27cf1d845e))
* **e2e:** hardcode ubuntu-latest for e2e-kind, fix go-version, add local run docs ([5f930fe](https://github.com/syscode-labs/imp/commit/5f930fe1865e7b5fdab65ea408dba2a75f6460e5))
* **e2e:** import strings for BeforeSuite node labeling ([c3afc89](https://github.com/syscode-labs/imp/commit/c3afc89b30ab4121a61fe3b22ac71395ac0ff02a))
* **e2e:** nil-guard port-forward kill, remove sleep, align GO_VERSION with go.mod ([da33ea9](https://github.com/syscode-labs/imp/commit/da33ea9aca06598c75c0088e37e7f0cc9dc315e6))
* **e2e:** pin operator replicas to one on single-node kind clusters ([2375165](https://github.com/syscode-labs/imp/commit/2375165eb5b8b6a7237c202e5a112dc31280ade2))
* **e2e:** point runtime DaemonSet at the locally built image ([5260cc1](https://github.com/syscode-labs/imp/commit/5260cc109331ec5123bdfdc374770f3da9252d36))
* **e2e:** resolve agent startup and node scheduling race ([3f36a16](https://github.com/syscode-labs/imp/commit/3f36a164b7c934c1e13d391af58165df8d5e6ec2))
* **e2e:** resolve agent startup and node scheduling race ([1d6efef](https://github.com/syscode-labs/imp/commit/1d6efefa221262d256c76364c93c7cd253afc422))
* **examples:** add required diskGiB to tiny-smoke class ([8c2d17e](https://github.com/syscode-labs/imp/commit/8c2d17ed2d6c6f495ab845cf11b9100e1dd3ee40))
* **examples:** make tiny-smoke connectivity check bounded and app-agnostic ([91e3050](https://github.com/syscode-labs/imp/commit/91e305028b1a8e7ee8add170b0cdfb5812238978))
* **examples:** use curl for port-forward readiness check (cross-platform) ([71b151c](https://github.com/syscode-labs/imp/commit/71b151c7fd84b29af1d794ff12987c88126ceb92))
* **examples:** wait for agent port-forward before exec ([d518dfd](https://github.com/syscode-labs/imp/commit/d518dfd4031821ded418f9261fa785a4a4e99d88))
* **guest:** guard cpu/iowait counter wrap, clarify cpuAndIOWaitUsage doc ([c547a74](https://github.com/syscode-labs/imp/commit/c547a741e3e5358bacc7ae90c84a47df92c83a7f))
* **guest:** use incoming context in Exec, Bavail for disk usage, add connection-refused test ([4b79426](https://github.com/syscode-labs/imp/commit/4b794267a6dc68f390a9315fb803415c104f4233))
* **guest:** use overflow-safe exit-code conversion (gosec G115) ([72ead23](https://github.com/syscode-labs/imp/commit/72ead23173b58fdc3677339fe644a96c8c85fc88))
* **helm:** agent DaemonSet — CharDevice type, updateStrategy, tolerations, test coverage ([efed262](https://github.com/syscode-labs/imp/commit/efed262f42b6b7d1dece86e09ed896b93813147e))
* **helm:** agent RBAC — block-style resources, strengthen test assertions ([516755f](https://github.com/syscode-labs/imp/commit/516755f08ad78e9f3a2349ae55a49de140b67f69))
* **helm:** bound SA name length to 63 chars after suffix ([a101f12](https://github.com/syscode-labs/imp/commit/a101f12bd406fcb81934a462b5bef9772e633777))
* **helm:** gate operator TLS volume mount on webhook.certManager.enabled ([a55abb8](https://github.com/syscode-labs/imp/commit/a55abb8b8057b02669f083556590da5f6368c94e))
* **helm:** remove extraneous hostPID/hostNetwork from agent DaemonSet ([cb6c82b](https://github.com/syscode-labs/imp/commit/cb6c82b4375d03327248fa588604c3e1691bc58c))
* **helm:** separate podMonitor toggle, quote interval values ([30dd0ee](https://github.com/syscode-labs/imp/commit/30dd0ee312b9cea4764e646f6a1156ee02733281))
* **helm:** webhook — template cert/secret names, gate on certManager.enabled, scope rules ([4e15a22](https://github.com/syscode-labs/imp/commit/4e15a22783d383af8da0950c3cdb89c3e8630c09))
* **lifecycle:** clarify one-shot completion semantics ([5d978ba](https://github.com/syscode-labs/imp/commit/5d978bae49afbf7b9d5186f10a9612105f9b5195))
* **lint:** apply goimports local-prefix formatting ([8466a00](https://github.com/syscode-labs/imp/commit/8466a00f6593ad6f195797496bb34600d19ee71f))
* **metrics:** clear stale state on SetVMState, proper server lifecycle via mgr.Add, wire Metrics in main ([08d1051](https://github.com/syscode-labs/imp/commit/08d105183757addacbbb946dbb304d0624a87a09))
* **metrics:** use sampleCount delta assertion; remove dead ResetMetricsForTest ([9588f34](https://github.com/syscode-labs/imp/commit/9588f344ef6d81c5e62edfb3baf5007c9519998c))
* **network:** coordinate IP claims across nodes ([#52](https://github.com/syscode-labs/imp/issues/52)) ([3cdc503](https://github.com/syscode-labs/imp/commit/3cdc5034aa54ecdeba8369545ed68910bf1ac84e))
* **network:** correct := to = for second LinkByName reassignment in EnsureVXLAN ([523178a](https://github.com/syscode-labs/imp/commit/523178a52df90c9a4ef2d2453a69393706df8027))
* **network:** removeNATIptables propagates errors; move FindNftHandle to export_test.go ([897d5ed](https://github.com/syscode-labs/imp/commit/897d5edf73e9cf9de4ed94009e837a3a92969145))
* **network:** Reserve increments vmCount for correct wasLast tracking ([81fc9d4](https://github.com/syscode-labs/imp/commit/81fc9d45a38ba38fd5ad4435448f1b39a5817b15))
* **network:** reuse LinkByName result in EnsureVXLAN to avoid double call ([cbd39ad](https://github.com/syscode-labs/imp/commit/cbd39adbf012ddaec85eb4f23f1aadaaf4024945))
* **operator:** support running with webhooks disabled ([0ac8288](https://github.com/syscode-labs/imp/commit/0ac82881db8f9340c231a312f67105200b3d09c4))
* **phase2:** address review findings — log casing, reserved/resident split, tests ([#26](https://github.com/syscode-labs/imp/issues/26)) ([b70d94e](https://github.com/syscode-labs/imp/commit/b70d94e76665613db888093a6374eb1ad69052eb))
* **probe:** patch on transition only; remove unused import in vsock test ([03c4c2c](https://github.com/syscode-labs/imp/commit/03c4c2ca7aefadf75ab4356001d69813866d528b))
* **rbac:** commit operator leases rule missed by darwin generation ([d2ef6eb](https://github.com/syscode-labs/imp/commit/d2ef6eb1172d12eeec3e267a5793696a96ef24bb))
* **release:** align release-please manifest ([ceb5b7c](https://github.com/syscode-labs/imp/commit/ceb5b7cdb3c84911506494d3848d33f8b6ad6979))
* **release:** align release-please manifest ([#46](https://github.com/syscode-labs/imp/issues/46)) ([e8e4a51](https://github.com/syscode-labs/imp/commit/e8e4a51ce57cc41ff6f5d473b2e585bad8bd38d1))
* **release:** recover release-please after duplicate v0.8.0 ([#67](https://github.com/syscode-labs/imp/issues/67)) ([dd9b58e](https://github.com/syscode-labs/imp/commit/dd9b58eaa4b0fe7877cdc72567a7a3a01e7ab9e0))
* **rootfs:** address extract.go quality issues (close error, nolint comments) ([57ac76f](https://github.com/syscode-labs/imp/commit/57ac76f2ff4f8c878583179621973cf08b7b5502))
* **rootfs:** avoid aliasing caller slice in BuildComposite filter ([92e169a](https://github.com/syscode-labs/imp/commit/92e169a12bebf659b5de94c3d2521b55de9f9d81))
* **rootfs:** preserve absolute symlinks during layer extract ([98dc836](https://github.com/syscode-labs/imp/commit/98dc836a0f33ad35bd897509ec48807866fbb701))
* **rootfs:** preserve tar hardlinks during layer extract ([b106962](https://github.com/syscode-labs/imp/commit/b10696272a6a3e8d765ae7193cfb191fd2c5d101))
* **rootfs:** safe args slice + error context in writeInit ([bdb9111](https://github.com/syscode-labs/imp/commit/bdb91117349949a8ab6c78019db2c08bf446eee9))
* **rootfs:** validate symlink targets + fix TestBuild_CacheHit to actually test Build() ([302a029](https://github.com/syscode-labs/imp/commit/302a029219c3ec0cd7e340aa79946c8eec68a7b3))
* **runtime:** allow network lookup ([#48](https://github.com/syscode-labs/imp/issues/48)) ([5a9f3ef](https://github.com/syscode-labs/imp/commit/5a9f3efc825df8ac4bca2f2eac0406ce1ca35604))
* **runtime:** close bootstrap gaps for clean micro1 validation ([c954eb2](https://github.com/syscode-labs/imp/commit/c954eb277c9df09dcd115d05110e394eac06cffd))
* **sandbox:** reconcile firewall on existing ImpNetwork (was create-only, left empty deny list) ([4f69a1b](https://github.com/syscode-labs/imp/commit/4f69a1b5a0815f5457ba5d44b512817b3107816a))
* **tracing:** use imp.agent tracer name in SpanFromVM ([d67c45b](https://github.com/syscode-labs/imp/commit/d67c45b846a167ae63701e2ad90d964f99a64437))

## [0.8.0](https://github.com/syscode-labs/imp/compare/v0.7.0...v0.8.0) (2026-08-29)


### Features

* **api:** add ImpNetworkAttachment CRD with LAN/VLAN allowlist types ([8ea0f3f](https://github.com/syscode-labs/imp/commit/8ea0f3f500da1facd844285f117cc3f0de0adbdc))
* **network:** add RBAC-gated LAN/VLAN attachment for microVMs ([035fa58](https://github.com/syscode-labs/imp/commit/035fa580da98f03029373702d4a60a416178aac5))
* **network:** gate LAN/VLAN attachment behind admission and node bindings ([42745c6](https://github.com/syscode-labs/imp/commit/42745c63fb77018d624ca7bbb13268f2ebe8499a))
* **sandbox:** guest file API, gateway data plane, LAN-attachment guard ([#63](https://github.com/syscode-labs/imp/issues/63)) ([dbfa4c6](https://github.com/syscode-labs/imp/commit/dbfa4c60d65439a44b8063bdc23ff575675df4d1))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([53259a8](https://github.com/syscode-labs/imp/commit/53259a8b553a3e360fd2984e2a14ce308ab65c91))
* **sandbox:** optional AI-agent sandbox add-on with tiered tenancy ([b2087aa](https://github.com/syscode-labs/imp/commit/b2087aa22dc4c1242efd9eb3823410ab15c889dc))
* **sandbox:** sandbox add-on (rebase + e2e harness fix) ([a0ec6a6](https://github.com/syscode-labs/imp/commit/a0ec6a6843909848931fc9d030a8522e771869cf))
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
