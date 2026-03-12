# Changelog

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
