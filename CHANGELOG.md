## [0.5.1](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.5.0...v0.5.1) (2026-11-25)


### Bug Fixes

* **forgejo:** follow pagination links built from ROOT_URL at the configured address ([85752ab](https://github.com/metapatrol/forgejo-github-bridge/commit/85752ab300eed46315b5beaed53cde02e9a5035d)), closes [#52](https://github.com/metapatrol/forgejo-github-bridge/issues/52)

# [0.5.0](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.4.0...v0.5.0) (2026-10-07)


### Bug Fixes

* **deps:** update module github.com/prometheus/client_model to v0.6.3 ([7648dfa](https://github.com/metapatrol/forgejo-github-bridge/commit/7648dfaac92a44222697ed1c58cf3a6bbdd357b9))


### Features

* /authors filters by side and kind ([cb388fc](https://github.com/metapatrol/forgejo-github-bridge/commit/cb388fc79db52df9bafe468fc6fa56a0d42a205e))
* a record of what the bridge wrote, for the console ([38b0213](https://github.com/metapatrol/forgejo-github-bridge/commit/38b02138bbddce4891612a4b88d03958059c1096))
* **chart:** alerts as an optional PrometheusRule ([d7e7140](https://github.com/metapatrol/forgejo-github-bridge/commit/d7e7140b76a61ffd42efc430d52833fec5c561a9))
* **console:** a comment pair says its kind, and a line comment links to its discussion ([c574c39](https://github.com/metapatrol/forgejo-github-bridge/commit/c574c3953a3f173aa40f3ff457b47d2b32777c0a))
* Forgejo line comments land on their lines on GitHub ([946c071](https://github.com/metapatrol/forgejo-github-bridge/commit/946c0712285a2fee78287dfcc5df6e4e97727bca))
* metrics for the rate-limit windows, database maintenance and the console ([1166fdc](https://github.com/metapatrol/forgejo-github-bridge/commit/1166fdcb622eb9770bd0859299f9354b034420bc))
* **store:** comment pairs know their kind ([05ccd5e](https://github.com/metapatrol/forgejo-github-bridge/commit/05ccd5ef108575180d2219eb0aacca21a7c1da55))
* **ui:** link every count, repository and contributor; an author typeahead ([de53ad4](https://github.com/metapatrol/forgejo-github-bridge/commit/de53ad49e94d3358d0240b6c67d56239a9e8b917))
* **ui:** the bridge's activity: its own page, the overview, a repository and a pair's history ([72d2614](https://github.com/metapatrol/forgejo-github-bridge/commit/72d2614d72dc5fd97f800506e113217ac4063c4b))
* **ui:** the history tells line comments from conversation comments ([dd35bd1](https://github.com/metapatrol/forgejo-github-bridge/commit/dd35bd1092e4d1031cd211afd27e7ac52586b560))

# [0.4.0](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.3.1...v0.4.0) (2026-10-06)


### Bug Fixes

* **ui:** a tab left open across a deployment reloads onto the new build ([281e4e0](https://github.com/metapatrol/forgejo-github-bridge/commit/281e4e03d7dbdc49f7db619e8a1d80b6dd2c932b))


### Features

* /repositories filters by pullRequests ([72ad2c2](https://github.com/metapatrol/forgejo-github-bridge/commit/72ad2c224e5853dffca31c1cfaedab6bc936ab33))
* authors and the rebase comment in the console API ([136a049](https://github.com/metapatrol/forgejo-github-bridge/commit/136a049ea7d2b61607e08d38fac91a9e995e2748))
* live console events and a system page ([dc7df73](https://github.com/metapatrol/forgejo-github-bridge/commit/dc7df730abbfa69a8646a8462b8eb42dbee3361c))
* queue entries name the kind of their pair ([dda1535](https://github.com/metapatrol/forgejo-github-bridge/commit/dda153573a493a219df67b2a673e8d57185a37bd))
* read-only console API, served on a listener of its own ([6ebebf1](https://github.com/metapatrol/forgejo-github-bridge/commit/6ebebf1f4ea1199c1ce6fadbd8e2fa8e1e97b93e))
* scheduled database maintenance, shown on the system page ([9d05aa5](https://github.com/metapatrol/forgejo-github-bridge/commit/9d05aa5f0d997642e3130c6e0a130854b6e3d49a))
* search, sort and cursor paging on every console list ([dcf9ae8](https://github.com/metapatrol/forgejo-github-bridge/commit/dcf9ae8233c4aa767cd2916e6d70c34070c46174))
* **store:** record each pair's public title and author ([ec6d9bf](https://github.com/metapatrol/forgejo-github-bridge/commit/ec6d9bf88d7f7f0a5af8518afc5da6063a0533ef))
* the GitHub rate limit in the console ([a278d44](https://github.com/metapatrol/forgejo-github-bridge/commit/a278d4470a168312053fc88368c46d18afd38f21))
* **ui:** a pair's history, and people ([7863668](https://github.com/metapatrol/forgejo-github-bridge/commit/78636685d32bccf498c5a34962d9042e18728906))
* **ui:** a repository typeahead, and contributors by name ([bd4ce0d](https://github.com/metapatrol/forgejo-github-bridge/commit/bd4ce0dba8e8e75f03ba114b799a0fa1d89bc71b))
* **ui:** every list searched, sorted and paged by the API ([fcdf67d](https://github.com/metapatrol/forgejo-github-bridge/commit/fcdf67df7822b6ea9cc9802d889d70cd92af43fd))
* **ui:** issue and pull request lists open on what is open ([2fe81bd](https://github.com/metapatrol/forgejo-github-bridge/commit/2fe81bdc19ec548ef545d352a7c4a7f30f60fc0e))
* **ui:** issues and pull requests apart ([66ce742](https://github.com/metapatrol/forgejo-github-bridge/commit/66ce742db03651a6a62feb70409b375c065d84c5))
* **ui:** live updates and a system page ([789e53a](https://github.com/metapatrol/forgejo-github-bridge/commit/789e53acc988b3f7a4004f59426ca43cfc8875f0))
* **ui:** no notice for failing entries; the queue tile says it ([82aac73](https://github.com/metapatrol/forgejo-github-bridge/commit/82aac736e1119f7bd4bceb76ca9156925003ea48))
* **ui:** overview tiles GitHub, issues, pull requests, queue ([824ec82](https://github.com/metapatrol/forgejo-github-bridge/commit/824ec82ef5688a71c8dcbb732f1d42b3a0f101c3))
* **ui:** overview tiles link to their pages, the GitHub rate limit, no pause ([7440388](https://github.com/metapatrol/forgejo-github-bridge/commit/74403889fe591408f69d3b86a47febb348e615b4))
* **ui:** people are contributors ([79d0328](https://github.com/metapatrol/forgejo-github-bridge/commit/79d03285fe0e43ff1a3f910413e9936910992f18))
* **ui:** repositories as a status list, problems first ([0a79e0c](https://github.com/metapatrol/forgejo-github-bridge/commit/0a79e0cf6218f1cd0b735a917d6c4995d3e9046b))
* **ui:** repositories as narrow tiles on the overview and their page ([eef42fa](https://github.com/metapatrol/forgejo-github-bridge/commit/eef42fac3d012e7b2feeb4f72ccf33f8d74e05a3))
* **ui:** repositories live on the overview ([890fcc7](https://github.com/metapatrol/forgejo-github-bridge/commit/890fcc7d5de7310a92ad847139afe29c593aeff0))
* **ui:** the console, on the metapatrol brand ([6d0b5b1](https://github.com/metapatrol/forgejo-github-bridge/commit/6d0b5b1f10dd601e6eaf736a067e29b89b356b06))
* **ui:** the maintenance rounds on the system page ([5b01a75](https://github.com/metapatrol/forgejo-github-bridge/commit/5b01a755c70feecc0cc688817ca05b64e79bec68))
* **ui:** the pull request list suggests only repositories that import them, by the API ([fb01f12](https://github.com/metapatrol/forgejo-github-bridge/commit/fb01f12cff4b8cb5e90eed110549d26f27741347))
* **ui:** the sidebar status is the console's own connection ([2dee93f](https://github.com/metapatrol/forgejo-github-bridge/commit/2dee93fa9303c2f3358f09a235b345d43a12d231))
* **ui:** the stream's state in the sidebar status, not in a header of its own ([3e00824](https://github.com/metapatrol/forgejo-github-bridge/commit/3e0082404aa683a0487e018aa0f8505e01a1df86))
* **ui:** the system page leads with service, database, GitHub and maintenance ([487b345](https://github.com/metapatrol/forgejo-github-bridge/commit/487b34527a1f1cab46a0c306306d15cdf57364ad))

## [0.3.1](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.3.0...v0.3.1) (2026-09-26)


### Bug Fixes

* check the contrib fork and rights before every push ([c1e54e5](https://github.com/metapatrol/forgejo-github-bridge/commit/c1e54e5097f47cbe0ab0f45d4b8a18fbcc6f62db))

# [0.3.0](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.2.0...v0.3.0) (2026-09-26)


### Bug Fixes

* **gitops:** wrap both errors when no token can be had ([60fa007](https://github.com/metapatrol/forgejo-github-bridge/commit/60fa007c6d5e53b43f2060f445b82b4ad1f58f5e))


### Features

* create and check the contrib fork from the bridge ([8f4ba93](https://github.com/metapatrol/forgejo-github-bridge/commit/8f4ba9329b53ed434027e02d6464aac43b702e1b))
* import pull requests from private GitHub repositories ([12796b5](https://github.com/metapatrol/forgejo-github-bridge/commit/12796b5e1059f9c7264ece76f88ca746ecaeb657))

# [0.2.0](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.1.0...v0.2.0) (2026-09-26)


### Features

* read credentials from files, and run in Docker without Kubernetes ([6dd4af3](https://github.com/metapatrol/forgejo-github-bridge/commit/6dd4af3e2634d463b5f5140c59c09d9dd5359fad))

# [0.1.0](https://github.com/metapatrol/forgejo-github-bridge/compare/v0.0.0...v0.1.0) (2026-09-25)


### Bug Fixes

* refuse shared contrib forks, and make stuck reconciles visible ([636b08e](https://github.com/metapatrol/forgejo-github-bridge/commit/636b08e69359b9e762644dd313d82dc820e0e8cb))
* **translate:** set placeholders as code spans ([619ffbf](https://github.com/metapatrol/forgejo-github-bridge/commit/619ffbfffa386c6afd215b79d7ca861619a99040))


### Features

* configurable messages, one notification per rebase, comments before close ([159877f](https://github.com/metapatrol/forgejo-github-bridge/commit/159877f2ac3f877f3e5c1659c079e28f4eabbb36))
* Grafana dashboard chart ([149610e](https://github.com/metapatrol/forgejo-github-bridge/commit/149610e878c30c9c0a0705b4e7a5898dcd701e73))
* import pull requests from GitHub into Forgejo ([07f4b1b](https://github.com/metapatrol/forgejo-github-bridge/commit/07f4b1bd1131b3466b45d7eb4f3384a3a4a32428))
