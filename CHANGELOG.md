# Changelog

## 1.0.0 (2026-04-23)


### Features

* add adapter implementations for GitHub, Slack, Teams, Jira, Confluence, Claude Code, Anthropic, Ollama ([e11382b](https://github.com/AaronL1011/spec-cli/commit/e11382b068b0eb6b422f996899dc99897246a299))
* add build step execution workflow ([83d810d](https://github.com/AaronL1011/spec-cli/commit/83d810d84f2d4d7132133ae285147e2cdfc3ef35))
* add install-completions to makefile for various shells ([e4d7246](https://github.com/AaronL1011/spec-cli/commit/e4d7246b0869ab572d6dbd2404e1084dfac9f4c1))
* add passive awareness and --mine filter ([fe21992](https://github.com/AaronL1011/spec-cli/commit/fe21992565298687c6892caf8e03b4639bfc2877))
* add release and CI pipeline ([ce9a61a](https://github.com/AaronL1011/spec-cli/commit/ce9a61a4edc19a34933a78c32c58b3e46acd51fb))
* add release and CI pipeline ([fe4063c](https://github.com/AaronL1011/spec-cli/commit/fe4063c81318dbe19b60b9eec715a990dbd03e74))
* add shell completion support ([a9299d3](https://github.com/AaronL1011/spec-cli/commit/a9299d33aa7a05ba9f338bb8f5559942c079fc65))
* add spec fix for fast-track bug fixes ([9d9eb63](https://github.com/AaronL1011/spec-cli/commit/9d9eb6305456957e172e4431f00b1c8183d00c8e))
* add spec push command to commit and push local spec edits ([719b69d](https://github.com/AaronL1011/spec-cli/commit/719b69de1587ab2e0ec96f514e40a357396a21c5))
* add technical planning workflow ([98b0085](https://github.com/AaronL1011/spec-cli/commit/98b00855f515ae089c734630e609b1de82fb4624))
* added install-man to makefile ([fd9eac8](https://github.com/AaronL1011/spec-cli/commit/fd9eac82e776b5722ee7dfdf46800ef65a63e448))
* configurable pipelines phase 1 ([c82e6d7](https://github.com/AaronL1011/spec-cli/commit/c82e6d7d5a2bcc325feff861d34968aa60388c57))
* effects executor for pipeline transitions (phase 4) ([201fd5d](https://github.com/AaronL1011/spec-cli/commit/201fd5dd0a5113f8218fbe9aadb253e366619a3a))
* expression engine for pipeline gates (phase 2) ([47ec256](https://github.com/AaronL1011/spec-cli/commit/47ec2560b580aab8c72ce7642ad3ee46ad3194da))
* extend config schemas for SPEC-005 ([dd4c7b6](https://github.com/AaronL1011/spec-cli/commit/dd4c7b6c9ee84d683805f3ff9e889f75b2208d52))
* extend frontmatter schema for build plans ([ec9b805](https://github.com/AaronL1011/spec-cli/commit/ec9b805d88b33b9130fc0f678ad84f9e68f32c4b))
* init spec cli tool ([08e6f17](https://github.com/AaronL1011/spec-cli/commit/08e6f17bb60238f537abacafae0a2b2a74751f15))
* interactive pipeline configuration (phase 3) ([172df63](https://github.com/AaronL1011/spec-cli/commit/172df63497598215c4b3f65124b372b67b173227))
* pipeline validation command (phase 6) ([32eb4c3](https://github.com/AaronL1011/spec-cli/commit/32eb4c363120ddb7ea7e54fcc9f817fc86ff8d01))
* skip_when and variants support (phase 5) ([fd9cb2f](https://github.com/AaronL1011/spec-cli/commit/fd9cb2fb3157695547daa24aa7d4e0b713589cf6))
* SPEC-004 Configurable Pipelines ([2262179](https://github.com/AaronL1011/spec-cli/commit/2262179a103a1f92ca93fbd178d858d6282d5d8b))
* SPEC-005 Frictionless engineer workflows ([4a8e76c](https://github.com/AaronL1011/spec-cli/commit/4a8e76c02bdc18e6f79a1a7b7114e2cd42c4d3be))


### Bug Fixes

* guard against nuking unpushed local spec edits ([410bacf](https://github.com/AaronL1011/spec-cli/commit/410bacf9cb673b39ec648e9cad36daf87fdcfb0f))
* invalidate dashboard cache after pipeline state changes ([14eeddd](https://github.com/AaronL1011/spec-cli/commit/14eeddd161d6961604919f8fec3d69b447d2e859))
* invalidate dashboard cache when spec files change on disk ([6bc080c](https://github.com/AaronL1011/spec-cli/commit/6bc080c2ec0a0abdc22ce38c23d018272c513c36))
* pilot readiness — SPEC_HOME, docs alignment, runbook ([8e12254](https://github.com/AaronL1011/spec-cli/commit/8e12254a396e3626c7eb23216150f121f7470a20))
* resolve bugs and robustness gaps from audit ([5fb8039](https://github.com/AaronL1011/spec-cli/commit/5fb8039e763563ed4cc1f10503b664f4c9fcb186))
* surface actionable errors from mcp-server on stderr ([2565ba4](https://github.com/AaronL1011/spec-cli/commit/2565ba4b6088c290284ee1e90c094b7b92dc4936))
* surface all errors to user, GITHUB_TOKEN auth for git operations ([a8a1a56](https://github.com/AaronL1011/spec-cli/commit/a8a1a56320f61d9f0a47660b3364b3a030f2d17e))
