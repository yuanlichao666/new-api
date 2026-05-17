# token1688 Ops Assets

This directory contains token1688-specific operational assets that must be
versioned together with the NewAPI fork.

This directory is the canonical release source for channel 47 operational
assets. Do not publish the channel 47 shim, systemd service, or env templates
from `newapi/30-ops/systemd`; those same-name files are historical operations
records only. Update and review this directory first, then copy approved
artifacts during an explicit rollout.

## Codex DeepSeek Channel Proxy

`shims/deepseek_codex_channel_proxy.py` is the channel-local proxy for the
emergency Codex -> DeepSeek fallback channel. NewAPI keeps billing and client
visibility on the requested GPT/Codex model name, while this proxy rewrites the
upstream model to `deepseek-v4-pro` and converts `/v1/responses` and
`/v1/chat/completions` to the upstream OpenAI-compatible chat API.

Operational templates:

- `systemd/newapi-codex-deepseek-channel-proxy.service`
- `systemd/newapi-codex-deepseek-channel-proxy.bj.env`
- `systemd/newapi-codex-deepseek-channel-proxy.hk.env`

The regional env templates bind to loopback by default. Expose the proxy only
through the local NewAPI host or an explicitly approved internal listener.
Startup logs intentionally record only whether an egress proxy is configured and
the proxy target host, never the full proxy URL or credentials.

The proxy depends on `cachetools` for bounded TTL key-value state. It must not
store full request or response JSON across requests.

Production rollout remains manual and must follow the external operations
runbook: back up first, test locally with production-like data/config, and only
then restart services after explicit approval.
