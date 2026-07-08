# Authentication

PikoCI supports two authentication methods: local username/password and OAuth/OIDC single sign-on. Both can be active simultaneously, and users can link multiple OAuth identities to a single PikoCI account.

## Overview

- **Local auth** uses username/password stored in PikoCI's database (bcrypt-hashed).
- **OAuth/OIDC auth** delegates authentication to an external identity provider (GitHub, Google, GitLab, Keycloak, etc.).
- Multiple OAuth providers can be active at the same time.
- Local auth can be disabled once at least one OAuth provider is configured.
- OAuth users are auto-created on first login with a profile completion step.
- Existing local users can link their OAuth identity from their profile.

## Configuring OAuth Providers

### Prerequisites

1. Set the `--external-url` flag on the PikoCI server to the public URL users access (e.g., `https://ci.example.com`). This is used to build OAuth callback URLs.

    ```bash
    pikoci server --jwt-secret my-secret --external-url https://ci.example.com
    ```

    Or via environment variable:

    ```bash
    export EXTERNAL_URL=https://ci.example.com
    ```

2. Log in as a global admin.

### Adding a Provider via the Admin UI

Go to the user menu (top right) and click **Authentication**. From there:

1. Click **Add Provider**.
2. Select a **Provider Template** (GitHub, Google, GitLab, etc.) to auto-fill URLs, scopes, and type. Or select **Other** for manual configuration.
3. The **Callback URL** is displayed automatically — copy it and paste it into your provider's redirect URI settings.
4. Fill in your **Client ID** and **Client Secret**.
5. Click **Create Provider**.

The provider appears on the login page immediately — no server restart needed.

### Provider Types

PikoCI supports two provider types:

| Type | When to use | What you configure |
|------|-------------|-------------------|
| **OIDC** | Google, GitLab, Keycloak, Azure AD, any OpenID Connect provider | Just the Issuer URL — endpoints are auto-discovered |
| **OAuth2** | GitHub, Bitbucket, custom providers without OIDC discovery | Auth URL, Token URL, and Userinfo URL manually |

---

## GitHub (OAuth2)

GitHub does not support OIDC, so use the **OAuth2** type.

### 1. Create a GitHub OAuth App

1. Go to **GitHub → Settings → Developer settings → OAuth Apps → New OAuth App**.
2. Fill in:
    - **Application name**: `PikoCI` (or whatever you like)
    - **Homepage URL**: `https://ci.example.com`
    - **Authorization callback URL**: `https://ci.example.com/auth/oauth/github/callback`
3. Click **Register application**.
4. Copy the **Client ID**.
5. Click **Generate a new client secret** and copy it.

### 2. Add to PikoCI

In the Authentication admin page, click **Add Provider**, select **GitHub** from the template (all URLs and settings are pre-filled), then paste your **Client ID** and **Client Secret**.

!!! note
    Set **Username Claim** to `login` to use the GitHub username. Set it to `email` to use the email prefix instead.

!!! warning
    GitHub OAuth Apps do not restrict which users can authorize. Any GitHub user can sign in unless you restrict access at the network level or use a GitHub Organization App. See [Restricting Who Can Sign In](#restricting-who-can-sign-in).

---

## Google (OIDC)

Google supports OpenID Connect, so use the **OIDC** type.

### 1. Create Google OAuth Credentials

1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Select or create a project.
3. Go to **APIs & Services → OAuth consent screen**. Set **User type** to **Internal** to restrict to your Google Workspace domain (recommended). External allows any Google account.
4. Go to **APIs & Services → Credentials → Create Credentials → OAuth client ID**.
5. Set **Application type** to **Web application**.
6. Under **Authorized redirect URIs**, add: `https://ci.example.com/auth/oauth/google/callback`
7. Click **Create** and copy the **Client ID** and **Client Secret**.

### 2. Add to PikoCI

In the Authentication admin page, click **Add Provider**, select **Google** from the template (issuer URL and settings are pre-filled), then paste your **Client ID** and **Client Secret**.

---

## GitLab (OIDC)

Works with both GitLab.com and self-hosted GitLab.

### 1. Create a GitLab Application

1. Go to **GitLab → Admin Area → Applications → New Application** (or **User Settings → Applications** for non-admin).
2. Fill in:
    - **Name**: `PikoCI`
    - **Redirect URI**: `https://ci.example.com/auth/oauth/gitlab/callback`
    - **Scopes**: `openid`, `email`, `profile`
3. Click **Save application** and copy the **Application ID** and **Secret**.

### 2. Add to PikoCI

In the Authentication admin page, click **Add Provider**, select **GitLab** from the template, then paste your **Client ID** and **Client Secret**. For self-hosted GitLab, change the **Issuer URL** to your instance's URL.

---

## Keycloak (OIDC)

### 1. Create a Keycloak Client

1. In Keycloak Admin Console, go to your realm → **Clients → Create client**.
2. Set **Client ID** to `pikoci`.
3. Set **Client authentication** to **On** (confidential).
4. Under **Valid redirect URIs**, add: `https://ci.example.com/auth/oauth/keycloak/callback`
5. Save and go to the **Credentials** tab to copy the **Client secret**.

### 2. Add to PikoCI

In the Authentication admin page, click **Add Provider**, select **Keycloak** from the template, then set the **Issuer URL** to `https://keycloak.example.com/realms/your-realm` and paste your **Client ID** and **Client Secret**.

---

## Microsoft / Azure AD (OIDC)

### 1. Register an Application in Azure AD

1. Go to **Azure Portal → Azure Active Directory → App registrations → New registration**.
2. Set **Redirect URI** to `https://ci.example.com/auth/oauth/microsoft/callback` (type: Web).
3. After creation, copy the **Application (client) ID** and **Directory (tenant) ID**.
4. Go to **Certificates & secrets → New client secret** and copy the value.

### 2. Add to PikoCI

In the Authentication admin page, click **Add Provider**, select **Microsoft** from the template, then set the **Issuer URL** to `https://login.microsoftonline.com/{tenant-id}/v2.0` (replace `{tenant-id}` with your Azure AD tenant ID) and paste your **Client ID** and **Client Secret**.

---

## Login Flow

When OAuth providers are configured, the login page shows the local login form followed by OAuth provider buttons below an "or" divider.

### New Users

1. User clicks **Log in with GitHub** (or another provider).
2. PikoCI redirects to the provider's authorization page.
3. User authenticates at the provider.
4. Provider redirects back to PikoCI with an authorization code.
5. PikoCI exchanges the code for an access token and fetches user info.
6. If this is the first login, PikoCI shows a **Complete Your Profile** form pre-filled with the username (from the provider) and full name.
7. User reviews/edits and submits. A PikoCI account is created and linked to the OAuth identity.
8. User is logged in.

### Returning Users

1. User clicks **Log in with GitHub**.
2. Same redirect flow.
3. PikoCI finds the existing link and logs the user in directly (no profile form).

## Restricting Who Can Sign In

PikoCI itself does not filter which users from a provider can sign in — any user who successfully authenticates with the provider can create a PikoCI account. **You should configure access restrictions at the provider level.**

Here's how for each provider:

| Provider | How to restrict access |
|----------|----------------------|
| **Google** | In Google Cloud Console → OAuth consent screen, set **User type** to **Internal**. Only users in your Google Workspace organization can authenticate. |
| **GitLab** | Self-hosted GitLab: only users with GitLab accounts can authenticate. GitLab.com: restrict the application to group members under **Admin → Applications**. |
| **Keycloak** | Only users in the configured realm can authenticate. Use realm roles, groups, or client scopes to restrict further. |
| **Azure AD** | Tenant-scoped by default — only users in your Azure AD tenant can authenticate. For multi-tenant apps, configure **Supported account types** to restrict. |
| **GitHub** | GitHub OAuth Apps allow **any** GitHub user to authorize. To restrict: create a **GitHub Organization App** under your org's settings and enable **Request user authorization (OAuth) during installation**, or use GitHub's IP allow lists. Alternatively, create users manually in PikoCI first and have them link their GitHub accounts from their profile (no auto-provisioning). |

!!! warning
    If you use GitHub as your only OAuth provider without additional restrictions, any GitHub user can create a PikoCI account. Consider keeping local auth enabled and managing users manually, or restricting at the network level.

## Account Linking

Existing local users can link OAuth identities from **Profile → Linked Accounts**:

1. Go to **Profile** and click the **Linked Accounts** tab.
2. Click **Link GitHub** (or another provider).
3. Authenticate at the provider.
4. The account is linked. You can now log in with either method.

To unlink, click the **Unlink** button next to the provider.

!!! warning
    You cannot unlink a provider if it is your only authentication method. Set a local password first or link another provider before unlinking.

## Setting a Local Password for OAuth Users

OAuth users are created without a local password. To enable local login:

1. Go to **Profile → Password** tab.
2. Since no password has been set yet, the "Current Password" field is hidden. Enter your new password and confirm it.
3. After setting a password, the "Current Password" field will appear for future changes.

This allows logging in with either OAuth or username/password.

## Admin: Auth Settings

### Disabling Local Auth

Global admins can disable local username/password login from the **Authentication** admin page by toggling **Enable local username/password login** off.

!!! warning
    Local auth cannot be disabled if any user relies solely on local password authentication. All users must have at least one OAuth provider linked before local auth can be turned off.

### Managing Providers

From the Authentication admin page, you can:

- **Add** new providers using preset templates or manual configuration
- **Edit** existing providers (client secret is preserved if left empty)
- **Enable/disable** providers without deleting them
- **Delete** providers

!!! warning
    You cannot disable or delete a provider if any user relies on it as their only authentication method (no local password and no other OAuth links). The affected username is shown in the error message.

Changes take effect immediately — no server restart needed.

## API Endpoints

### Unauthenticated

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/auth/methods` | List enabled providers and local auth status |
| `GET` | `/auth/oauth/{canonical}` | Start OAuth flow (redirects to provider) |
| `GET` | `/auth/oauth/{canonical}/callback` | Handle provider callback |
| `POST` | `/auth/oauth/complete-profile` | Complete profile for first-time OAuth login |

### Authenticated

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/profile/linked-accounts` | List linked OAuth accounts |
| `DELETE` | `/profile/linked-accounts/{canonical}` | Unlink an OAuth account |

### Admin (Global Admin)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/oauth-providers` | List all providers |
| `POST` | `/admin/oauth-providers` | Create a provider |
| `PUT` | `/admin/oauth-providers/{canonical}` | Update a provider |
| `DELETE` | `/admin/oauth-providers/{canonical}` | Delete a provider |
| `GET` | `/admin/auth-settings` | Get auth settings |
| `PUT` | `/admin/auth-settings` | Update auth settings |

## Security

- **CSRF protection**: Each OAuth flow uses a unique random state parameter with a 5-minute expiration.
- **Token security**: OAuth callback tokens are short-lived (5 minutes) and single-use.
- **Client secrets**: Stored in the database (same security model as pipeline secrets). Never exposed in API responses.
- **Callback URLs**: Built from the `--external-url` flag to prevent open redirect attacks.
- **Lockout prevention**: PikoCI prevents actions that would lock users out of their accounts — disabling local auth, disabling/deleting a provider, or unlinking an account are all blocked if any user would be left with no way to log in.

See also: [Roles & Permissions](Roles.md) · [API Tokens](API-Tokens.md) · [Server Configuration](Server.md)
