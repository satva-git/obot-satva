# Satva Obot: how changes reach obot.satva.xyz

`satva/main` = an upstream Obot release tag + Satva's patches. The Coolify app `obot` builds this
branch itself (build pack Dockerfile, public repo `satva-git/obot-satva`, no registry login).
Data lives on the Coolify volume `/data`, never in the image.

| I want to | Do this |
|---|---|
| Ship a change | PR into `satva/main`, wait for the green `satva` test check, merge. Then Coolify → obot → **Deploy**. The pre-deployment command backs up the DB to `/data/obot-pre-deploy.sql`; the old container keeps serving until the new build starts. Build takes 10–20 min on the server. |
| Roll back | Revert the commit on `satva/main`, then Deploy. Fastest emergency: Coolify → obot → Deployments → redeploy an earlier finished deployment. |
| Upgrade Obot | `bash .github/satva/upgrade.sh v0.25.6` in the local clone, wait for CI on `satva/upgrade-v0.25.6`, open the PR it prints. Also bump the provider tags at the top of `Dockerfile`. Read the release notes first: a minor-version jump migrates the DB one way. |
| Restore the DB | Coolify → obot → Terminal: `psql -U obot -d obot -f /data/obot-pre-deploy.sql` (into an empty DB) or the nightly file under `/data/backups/`. |

Old settings (Docker Image `ghcr.io/obot-platform/obot:v0.25.1`) are saved outside the repo; to go back,
set Build Pack = Docker Image with that image in Coolify and Deploy.
