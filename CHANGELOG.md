# Changelog

## [1.3.2](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.3.1...v1.3.2) (2026-03-23)


### Bug Fixes

* push Helm chart to separate OCI path to avoid collision with Docker image ([#14](https://github.com/unaiur/k8s-spark-ui-assist/issues/14)) ([a92f2d7](https://github.com/unaiur/k8s-spark-ui-assist/commit/a92f2d7e9cadc9ec2bdf1e5198d77386eb45289c))

## [1.3.1](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.3.0...v1.3.1) (2026-03-23)


### Bug Fixes

* add CMD ["--help"] to Dockerfile so bare image run prints usage ([0fae370](https://github.com/unaiur/k8s-spark-ui-assist/commit/0fae3701336529cd3e3d682cf33228932139898c))
* add explicit command to deployment to avoid missing ENTRYPOINT in older images ([89b44ca](https://github.com/unaiur/k8s-spark-ui-assist/commit/89b44ca0608a552539333d894bff901c3fb950f1))

## [1.3.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.2.0...v1.3.0) (2026-03-23)


### Features

* add Helm chart with RBAC, Service, Deployment and HTTPRoute ([7b779e4](https://github.com/unaiur/k8s-spark-ui-assist/commit/7b779e4df41ba5bfbebd6da7136c6918ba88cd5c))
* add semver releases, conventional commit validation, and default GO_VERSION ([b035e96](https://github.com/unaiur/k8s-spark-ui-assist/commit/b035e96de14265205373e51c1a554bbc2e16370e))
* expand http gateway values with tpl to support global variable references ([#9](https://github.com/unaiur/k8s-spark-ui-assist/issues/9)) ([022b15e](https://github.com/unaiur/k8s-spark-ui-assist/commit/022b15e0f1923f0e300adf315498bccc418242a5))


### Bug Fixes

* add extraEnv support to helm chart ([b7d1104](https://github.com/unaiur/k8s-spark-ui-assist/commit/b7d11047b4b616780be66e5146a1c0de956875e4))
* add extraVolumes and extraVolumeMounts support to helm chart ([ab2f294](https://github.com/unaiur/k8s-spark-ui-assist/commit/ab2f29457e9a1fbb36d7bcdae21dc2c9ca81da84))
* create git tag in workflow since GitHub Releases API is blocked by token permissions ([cd9f165](https://github.com/unaiur/k8s-spark-ui-assist/commit/cd9f1657defec281aef2d64977d9e59c7ab68d63))
* docker and helm artifacts were not published on release ([1c71a93](https://github.com/unaiur/k8s-spark-ui-assist/commit/1c71a93d5ec9098ab06a3fefce285817bd93c30a))
* docker and helm artifacts were not published on release ([e79f3cb](https://github.com/unaiur/k8s-spark-ui-assist/commit/e79f3cb9c44e32750e21e4e4e6f5d2feae8b6f2a))
* move release-please config under packages key so version files are updated ([87afffb](https://github.com/unaiur/k8s-spark-ui-assist/commit/87afffbd52c75061b1714da398d29757fd2ebfb2))
* remove irrelevant volume configuration ([e704ef6](https://github.com/unaiur/k8s-spark-ui-assist/commit/e704ef62898f202cdd07059aae2784fdcc4c0169))
* update chart version and appVersion to 1.1.0 ([45f7abb](https://github.com/unaiur/k8s-spark-ui-assist/commit/45f7abb2b2952b188e0eb904dd7008b458c49dbf))


### Performance Improvements

* replace typed clientsets with dynamic client to reduce RSS ([09e7f93](https://github.com/unaiur/k8s-spark-ui-assist/commit/09e7f935efa4c8777488c8e35d59d0f4d26fd17a))

## [1.2.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.5...v1.2.0) (2026-03-23)


### Features

* expand http gateway values with tpl to support global variable references ([#9](https://github.com/unaiur/k8s-spark-ui-assist/issues/9)) ([022b15e](https://github.com/unaiur/k8s-spark-ui-assist/commit/022b15e0f1923f0e300adf315498bccc418242a5))

## [1.1.5](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.4...v1.1.5) (2026-03-22)


### Bug Fixes

* remove irrelevant volume configuration ([e704ef6](https://github.com/unaiur/k8s-spark-ui-assist/commit/e704ef62898f202cdd07059aae2784fdcc4c0169))

## [1.1.4](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.1.3...v1.1.4) (2026-03-22)


### Bug Fixes

* add extraVolumes and extraVolumeMounts support to helm chart ([ab2f294](https://github.com/unaiur/k8s-spark-ui-assist/commit/ab2f29457e9a1fbb36d7bcdae21dc2c9ca81da84))

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
