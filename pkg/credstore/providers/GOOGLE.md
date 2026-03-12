# Google Cloud / Workspace OAuth Setup

Google uses OAuth 2.0 for both Workspace APIs (Calendar, Gmail, Drive, Contacts)
and GCP APIs. Goth's `google` provider handles all of this.

## Create OAuth Client

1. Go to [Google Cloud Console → APIs & Services → Credentials](https://console.cloud.google.com/apis/credentials)
2. Click **"+ CREATE CREDENTIALS" → "OAuth client ID"**
3. Select **Application type**:
   - **Desktop app** — for local Genie
   - **Web application** — for remote Genie
4. Set **Authorized redirect URIs**:
   - Local: `http://localhost:8765`
   - Remote: `https://your-genie-server.com/oauth/callback`
5. Note the **Client ID** and **Client Secret**

## Enable APIs

In the same Google Cloud project, enable the APIs you need:

| API | Console Link |
|-----|-------------|
| Google Calendar API | [Enable](https://console.cloud.google.com/apis/library/calendar-json.googleapis.com) |
| People API (Contacts) | [Enable](https://console.cloud.google.com/apis/library/people.googleapis.com) |
| Gmail API | [Enable](https://console.cloud.google.com/apis/library/gmail.googleapis.com) |
| Google Drive API | [Enable](https://console.cloud.google.com/apis/library/drive.googleapis.com) |

## Scopes Reference

| Scope | Description |
|-------|-------------|
| `https://www.googleapis.com/auth/calendar` | Read/write Calendar |
| `https://www.googleapis.com/auth/contacts.readonly` | Read Contacts |
| `https://www.googleapis.com/auth/gmail.readonly` | Read Gmail |
| `https://www.googleapis.com/auth/gmail.send` | Send email |
| `https://www.googleapis.com/auth/drive.readonly` | Read Drive files |
| `https://www.googleapis.com/auth/userinfo.email` | User email |
| `https://www.googleapis.com/auth/userinfo.profile` | User profile |

## Registration

```go
import "github.com/markbates/goth/providers/google"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "google",
    Provider: google.New(
        clientID, clientSecret, callbackURL,
        "https://www.googleapis.com/auth/calendar",
        "https://www.googleapis.com/auth/gmail.readonly",
        "https://www.googleapis.com/auth/drive.readonly",
        "https://www.googleapis.com/auth/userinfo.email",
        "https://www.googleapis.com/auth/userinfo.profile",
    ),
})
```

## OAuth Consent Screen

For external users, configure the consent screen:

1. Go to [APIs & Services → OAuth consent screen](https://console.cloud.google.com/apis/credentials/consent)
2. Select **External** (or **Internal** for Workspace orgs)
3. Add scopes and submit for verification (or use test users during development)
