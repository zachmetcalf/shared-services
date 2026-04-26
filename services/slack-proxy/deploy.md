### slack-proxy

slack-proxy deploy

## setup

```powershell
if (!(Test-Path services/slack-proxy/.env)) { Copy-Item services/slack-proxy/.env.example services/slack-proxy/.env }
if (!(Test-Path "$HOME/.ssh/slack_proxy_deploy")) { ssh-keygen -t ed25519 -f "$HOME/.ssh/slack_proxy_deploy" -C slack-proxy-deploy -N '""' }
gh auth login
gcloud auth login
gcloud auth application-default login
```

fill `.env`

## apply

```powershell
$projectId = "YOUR_PROJECT_ID"
cd infra/slack-proxy
gcloud services enable compute.googleapis.com --project $projectId
terraform init
terraform validate
terraform apply -var="project_id=$projectId"
```

## secrets

```powershell
$domain = "YOUR_DOMAIN"
$gceHost = terraform output -raw gce_host
$gceUser = terraform output -raw gce_user
$key = Get-Content "$HOME/.ssh/slack_proxy_deploy" -Raw
$envFile = Get-Content ../../services/slack-proxy/.env -Raw
gh secret set GCE_HOST --body "$gceHost"
gh secret set GCE_USER --body "$gceUser"
gh secret set GCE_SSH_KEY --body "$key"
gh secret set SLACK_PROXY_DOMAIN --body "$domain"
gh secret set SLACK_PROXY_ENV_FILE --body "$envFile"
```

run `Slack-Proxy Deploy` from github actions

## test

```powershell
$domain = "YOUR_DOMAIN"
curl.exe "https://$domain/ping"
```

endpoint: `https://YOUR_DOMAIN/v1/slack`

## pause

```powershell
$projectId = "YOUR_PROJECT_ID"
$zone = "us-west1-a"
gcloud compute instances stop slack-proxy --zone $zone --project $projectId
```

## shutdown

```powershell
$projectId = "YOUR_PROJECT_ID"
cd infra/slack-proxy
terraform destroy -var="project_id=$projectId"
```

update dns and `GCE_HOST` if the ip changes
