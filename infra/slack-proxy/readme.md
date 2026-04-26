### slack-proxy infrastructure

terraform for `slack-proxy` gce vm

resources:
- static external ip
- `e2-micro` ubuntu vm
- ssh/http/https firewall rules
- docker + caddy runtime setup

secrets:
- github actions, not terraform

deploy:
`../../services/slack-proxy/deploy.md`
