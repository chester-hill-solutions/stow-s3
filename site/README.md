# Stow website

The static marketing site is hosted by the `stow-site` Cloudflare Worker in the
Chester Hill Solutions account at https://stow.chesterhillsolutions.ca.
The checked-in configuration serves only website assets; local preview and
rendering scripts are excluded from uploads.

After reviewing site changes, deploy from the repository root with an
authenticated Wrangler v4 installation:

```sh
wrangler deploy --config site/wrangler.jsonc --dry-run
wrangler deploy --config site/wrangler.jsonc
```

Preview locally with `python3 site/serve.py`. Agent-readable instructions are
available at `/agent.md`, and `/llms.txt` links to the current API contracts.
