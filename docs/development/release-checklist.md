## Release Checklist

- [ ] Update version number in package.json if not done after last release.
- [ ] Tag release branch. Wait for the CI to build artifacts.
- [ ] Sign windows installer.

### Sign mac installer (As there's an issue with the zip produced by the build script, we need to manually build and zip)
- [ ] Make sure the required env variables are set for the notarize, signing process.
- [ ] git clean, reset to make sure a clean (CI equivalent) build.
- [ ] Manually zip the installer.

### Release Documentation
- [ ] Release notes. Update on the GitHub draft Release page.
- [ ] docs update (Help, Readme..)
- [ ] Slack Announcements
- [ ] Newsletter summary
- [ ] Update metrics, roadmap on Confluence page

### Release
- [ ] Upload the release artifacts to the GitHub draft Release page under the names the build gave them.
  - From `yarn sign`'s `dist/`: the macOS dmg, zip, and `rdd` for both architectures, the Windows msi and `rdd.exe`, and the `.sha512sum` of each. The Package run's copies under the same names are unsigned.
  - From the Package run: the Linux zip and `rdd`. Create a `.sha512sum` for each.
- [ ] Perform smoke test on release artifacts.
- [ ] Update the release version for upgrade responder.
- [ ] Move from draft release to Release.
- [ ] Check the auto update functionality.
