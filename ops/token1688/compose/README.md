# Token1688 Dual-Container Compose

Token1688/NewAPI deployments use two immutable images from the same GitHub Actions run:

- `ghcr.io/yuanlichao666/new-api@sha256:...` for the Go API backend
- `ghcr.io/yuanlichao666/new-api-web@sha256:...` for the static web nginx container

Deploy by digest only. Do not deploy `latest`. Shadow and production cutover both
move as an API/Web pair.

The web container serves `web/dist` and proxies API prefixes to the backend. Host
Nginx should terminate TLS, apply security deny rules, and forward traffic to the
web container. Do not use a bare host `dist` directory as the default release
surface.
