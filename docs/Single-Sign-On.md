# Single sign-on (OpenID Connect)

GeoTracker can sign people in through any OpenID Connect provider: Authelia, Authentik, Keycloak, Pocket ID, Zitadel, Google, and others. It uses the authorization-code flow with PKCE. Password sign-in keeps working alongside it, so you can't lock yourself out.

## Set it up

1. **At your provider**, create a *confidential* client (web application) with:
   - **Redirect URI:** `https://your.domain/api/v1/auth/oidc/callback` (shown in Admin → Settings → Single sign-on; it is built from `GT_BASE_URL`, so set that first)
   - **Scopes:** `openid profile email`
2. In GeoTracker: **Admin → Settings → Single sign-on**. Enter the **issuer URL** (for example `https://auth.example.com`), the **client ID** and **client secret**, then enable it.
3. The login page now shows a single sign-on button.

## How people are matched

- Users are linked by the provider's stable `sub` claim.
- On first sign-in, a person is matched to an existing account by **verified email**.
- Turn on **Create accounts automatically** to let anyone your provider accepts sign in. Leave it off to allow only people an admin already added.

## Provider notes

| Provider | Issuer URL looks like |
|---|---|
| Authelia | `https://auth.example.com` |
| Authentik | `https://authentik.example.com/application/o/<slug>/` |
| Keycloak | `https://keycloak.example.com/realms/<realm>` |
| Pocket ID | `https://id.example.com` |
| Zitadel | `https://<instance>.zitadel.cloud` |

The issuer must serve `/.well-known/openid-configuration`; test it in a browser.

## Troubleshooting

| Symptom | Fix |
|---|---|
| "redirect_uri mismatch" at the provider | The URI at the provider must match exactly, including `https` and no trailing slash. Check `GT_BASE_URL`. |
| Sign-in loops back to the login page | Your provider didn't return a verified email, or *Create accounts automatically* is off and no account has that email. |
| "single sign-on is not configured" | The issuer can't be reached from the server, or the client ID or secret is empty. Check `docker logs geotracker`. |
