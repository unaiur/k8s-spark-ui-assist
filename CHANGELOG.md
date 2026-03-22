# Changelog

## [1.1.3](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.2...v1.1.3) (2026-03-22)


### Bug Fixes

* add extraEnv support to helm chart ([b7d1104](https://github.com/unaiur/k8s-spark-ui-assist/commit/b7d11047b4b616780be66e5146a1c0de956875e4))

## [1.1.2](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.1...v1.1.2) (2026-03-22)


### Bug Fixes

* create git tag in workflow since GitHub Releases API is blocked by token permissions ([cd9f165](https://github.com/unaiur/k8s-spark-ui-assist/commit/cd9f1657defec281aef2d64977d9e59c7ab68d63))

## [1.1.1](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.0...v1.1.1) (2026-03-22)


### Bug Fixes

* docker and helm artifacts were not published on release ([1c71a93](https://github.com/unaiur/k8s-spark-ui-assist/commit/1c71a93d5ec9098ab06a3fefce285817bd93c30a))
* docker and helm artifacts were not published on release ([e79f3cb](https://github.com/unaiur/k8s-spark-ui-assist/commit/e79f3cb9c44e32750e21e4e4e6f5d2feae8b6f2a))
* move release-please config under packages key so version files are updated ([87afffb](https://github.com/unaiur/k8s-spark-ui-assist/commit/87afffbd52c75061b1714da398d29757fd2ebfb2))
* update chart version and appVersion to 1.1.0 ([45f7abb](https://github.com/unaiur/k8s-spark-ui-assist/commit/45f7abb2b2952b188e0eb904dd7008b458c49dbf))

## [1.1.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.0.0...v1.1.0) (2026-03-22)


### Features

* add Helm chart with RBAC, Service, Deployment and HTTPRoute ([7b779e4](https://github.com/unaiur/k8s-spark-ui-assist/commit/7b779e4df41ba5bfbebd6da7136c6918ba88cd5c))


### Performance Improvements

* replace typed clientsets with dynamic client to reduce RSS ([09e7f93](https://github.com/unaiur/k8s-spark-ui-assist/commit/09e7f935efa4c8777488c8e35d59d0f4d26fd17a))

## 1.0.0 (2026-03-21)


### Features

* add semver releases, conventional commit validation, and default GO_VERSION ([b035e96](https://github.com/unaiur/k8s-spark-ui-assist/commit/b035e96de14265205373e51c1a554bbc2e16370e))
