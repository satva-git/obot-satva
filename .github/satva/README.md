# Satva Obot: how changes reach obot.satva.xyz

`satva/main` = an upstream Obot release tag + Satva's patches. Live (Coolify app `obot`) runs
`ghcr.io/satva-git/obot-satva:sha-<commit>`. Data lives on the Coolify volume `/data`, never in the image.

| I want to | Do this |
|---|---|
| Ship a change | PR into `satva/main`, merge. Actions tests + builds, then waits for **Approve** under Actions → the run → Review deployments. Deploy backs up the DB (`/data/obot-pre-deploy.sql`), swaps the image, health-checks, rolls back by itself on failure. |
| Roll back | Actions → satva → Run workflow on `satva/main` with `tag` = an earlier `sha-…` (see Packages → obot-satva). |
| Upgrade Obot | `bash .github/satva/upgrade.sh v0.25.6` in the local clone, wait for CI on `satva/upgrade-v0.25.6`, then run the push command it prints. Read the release notes first: a minor-version jump migrates the DB one way. |
| Restore the DB | Coolify → obot → Terminal: `psql -U obot -d obot -f /data/obot-pre-deploy.sql` (into an empty DB) or the nightly file under `/data/backups/`. |

One-time setup (already done once; repeat only on a new server): `docker login ghcr.io` on the Coolify
server with a `read:packages` token; secret `COOLIFY_TOKEN` in the `production` environment;
nightly backup as a Coolify Scheduled Task on the obot app.
