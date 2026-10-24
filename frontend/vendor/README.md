# Vendored build-time files

Files committed here so the frontend build never downloads code from a CDN. Every file is pinned to an exact
version and verified against the npm registry tarball.

| File | Source | Version | SHA-256 | License |
| --- | --- | --- | --- | --- |
| `inlang-plugin-message-format-4.4.5.js` | `@inlang/plugin-message-format` (`dist/index.js`) | 4.4.5 | `b22cf60eb28b3c8c3ce1fb6300611a0552f12d0d995d37c4dd2c96e3ad80c645` | MIT (`inlang-plugin-message-format.LICENSE`) |

The inlang message-format plugin teaches Paraglide JS to read `messages/<locale>.json`. It is referenced from
`project.inlang/settings.json`; Paraglide would otherwise download it from a CDN on every compile.

To upgrade it:

```bash
npm pack @inlang/plugin-message-format@<version>   # integrity-checked by npm
tar -xzf inlang-plugin-message-format-<version>.tgz package/dist/index.js package/LICENSE
```

Then copy `package/dist/index.js` here as `inlang-plugin-message-format-<version>.js`, update `settings.json` and
this table, and run `npm run check` and `npm run build`.
