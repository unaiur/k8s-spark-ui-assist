# Changelog

## [2.0.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.5.1...v2.0.0) (2026-03-26)


### ⚠ BREAKING CHANGES

* replace shared HTTPRoute with per-driver HTTPRoutes ([#27](https://github.com/unaiur/k8s-spark-ui-assist/issues/27))
* remove configurable driver path prefix, document spark.ui.reverseProxy ([#25](https://github.com/unaiur/k8s-spark-ui-assist/issues/25))

### Features

* add GET /proxy/api/{appID} endpoint to query Spark driver state ([a8adb83](https://github.com/unaiur/k8s-spark-ui-assist/commit/a8adb838d6d98f3e0e45ba5f2c2dbc3b7f58f207))
* add GET /proxy/api/reconcile endpoint ([#31](https://github.com/unaiur/k8s-spark-ui-assist/issues/31)) ([c552d83](https://github.com/unaiur/k8s-spark-ui-assist/commit/c552d830f8aea5939cdb3faac38ad957314894d5))
* gate HTTPRoute creation on pod Running phase ([c40be6f](https://github.com/unaiur/k8s-spark-ui-assist/commit/c40be6f60d33ca7f1bc87010d2ba964729d5a7b7))
* manage SHS root HTTPRoute based on Endpoints availability ([#34](https://github.com/unaiur/k8s-spark-ui-assist/issues/34)) ([5ef5987](https://github.com/unaiur/k8s-spark-ui-assist/commit/5ef5987cabfb8d1a1429628c857657e21b0e3d63))
* move dashboard to /proxy/, redirect all other paths there ([#28](https://github.com/unaiur/k8s-spark-ui-assist/issues/28)) ([a769b85](https://github.com/unaiur/k8s-spark-ui-assist/commit/a769b856ce35986c3f6b8c6ad49a6afcfcddc8c1))
* remove configurable driver path prefix, document spark.ui.reverseProxy ([#25](https://github.com/unaiur/k8s-spark-ui-assist/issues/25)) ([1735d7b](https://github.com/unaiur/k8s-spark-ui-assist/commit/1735d7b193daf1a5406f4f237dc73fd31f5e785b))
* replace shared HTTPRoute with per-driver HTTPRoutes ([#27](https://github.com/unaiur/k8s-spark-ui-assist/issues/27)) ([3546486](https://github.com/unaiur/k8s-spark-ui-assist/commit/35464868be865bd2c26cb16a26f89c28c465a63b))
* serve contextual status page at /proxy/&lt;appID&gt;/ when HTTPRoute is absent ([#33](https://github.com/unaiur/k8s-spark-ui-assist/issues/33)) ([70ec9fb](https://github.com/unaiur/k8s-spark-ui-assist/commit/70ec9fb0cf758b462308b588271d389f3a9d9938))


### Bug Fixes

* address Copilot review comments on spark-job-api ([1b8b3e3](https://github.com/unaiur/k8s-spark-ui-assist/commit/1b8b3e399145cc61f43ca50070ee0bb907f9c855))
* grant create, delete, list on httproutes in RBAC Role ([0350166](https://github.com/unaiur/k8s-spark-ui-assist/commit/0350166a199b47140467064486c69a5f2c47cbdb))
* pending pod state derives waiting reason from conditions, not phase ([99361f7](https://github.com/unaiur/k8s-spark-ui-assist/commit/99361f794c218e4cb1d0f0f4f6c8b4849929d17d))
* protect recordingHandler with mutex to eliminate data races under -race ([6f8bfdd](https://github.com/unaiur/k8s-spark-ui-assist/commit/6f8bfdd66f34f6ccb22db3358edd9a19e8fc9847))
* rename endpoint to /proxy/api/state/{appID} ([879d179](https://github.com/unaiur/k8s-spark-ui-assist/commit/879d179082ff132313b7b4f7de0686173590cce3))

## [1.5.1](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.5.0...v1.5.1) (2026-03-23)


### Bug Fixes

* append /jobs/ to driver links so Spark UI opens on the jobs page ([#23](https://github.com/unaiur/k8s-spark-ui-assist/issues/23)) ([ea24c8d](https://github.com/unaiur/k8s-spark-ui-assist/commit/ea24c8dde441c3d87ead4697f1a5505182ca0ab7))

## [1.5.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.4.1...v1.5.0) (2026-03-23)


### Features

* make driver path prefix configurable (default /proxy/) ([#21](https://github.com/unaiur/k8s-spark-ui-assist/issues/21)) ([8033f31](https://github.com/unaiur/k8s-spark-ui-assist/commit/8033f31d185d3c6056046eed1066c575d4f5d9d6))

## [1.4.1](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.4.0...v1.4.1) (2026-03-23)


### Bug Fixes

* grant update (not create/delete) on httproutes in RBAC ([#19](https://github.com/unaiur/k8s-spark-ui-assist/issues/19)) ([c17a450](https://github.com/unaiur/k8s-spark-ui-assist/commit/c17a450df702209ff158da4c1f8d155ed236bfbf))

## [1.4.0](https://github.com/unaiur/k8s-spark-ui-assist/compare/v1.3.2...v1.4.0) (2026-03-23)


### Features

* single shared HTTPRoute for dashboard and all Spark driver UIs ([#16](https://github.com/unaiur/k8s-spark-ui-assist/issues/16)) ([c85db6d](https://github.com/unaiur/k8s-spark-ui-assist/commit/c85db6d7b92b4fd60e5a7b518d3f11bed027a7dc))


### Bug Fixes

* redirect non-root requests to '/' in dashboard handler ([#18](https://github.com/unaiur/k8s-spark-ui-assist/issues/18)) ([35a1628](https://github.com/unaiur/k8s-spark-ui-assist/commit/35a16288ca466e44fe2d781122ef25c6a1df24eb))

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
