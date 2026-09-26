Put the credentials here, one per file, readable only by you:

| File | Contents |
|---|---|
| `github-app.pem` | the GitHub App's private key |
| `forgejo-token` | the `github-bridge` account's token |
| `forgejo-contrib-token` | the `github-contrib` account's token (pull requests only) |

```bash
chmod 700 . && chmod 600 github-app.pem forgejo-token forgejo-contrib-token
```
