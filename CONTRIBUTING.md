# Contributing

Issues and pull requests are welcome.

## Contributor License Agreement

Before we can merge your first pull request, you need to sign the [Contributor License Agreement](CLA.md). It's modeled on the Apache Individual CLA: you keep the rights to your contribution and grant Omni a license to use and distribute it.

When you open a pull request, the CLA Assistant bot comments with a link. Sign in with GitHub to sign; it takes a few seconds and covers all your future contributions. The PR's `license/cla` check passes once you've signed.

If you're contributing on behalf of your employer, make sure you're allowed to (see section 4 of the CLA). If your employer needs a Corporate CLA, open an issue and we'll follow up.

## Making changes

See [DEVELOPMENT.md](DEVELOPMENT.md) for building, testing, and updating the OpenAPI spec. Commands are generated from the spec, so most API changes need no code: they arrive with the next spec sync.

Keep pull requests focused on one change, include tests where it makes sense, and run `make test` before you push.
