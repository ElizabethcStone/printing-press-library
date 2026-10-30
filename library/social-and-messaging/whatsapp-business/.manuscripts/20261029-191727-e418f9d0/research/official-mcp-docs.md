# WhatsApp Business Tools MCP


For developers building on the WhatsApp Business Platform (Cloud API) — the WhatsApp Business Tools MCP server lets your AI agent and IDE assistant manage and debug your WhatsApp Business integration from one place. It enables an agent to discover the businesses and WhatsApp accounts you administer, onboard and register phone numbers, create and manage message templates, configure webhooks, and send messages — acting as you, on the apps and businesses you administer.

**Note:** **Note:** The WhatsApp Business Tools MCP server is rolling out gradually and may not be available to everyone yet. The interface and tool set are in beta and may change.

## Overview

The following table summarizes the server details:

| Attribute | Value |
|---|---|
| Server name | `WhatsApp Business Tools` |
| Transport | Streamable HTTP |
| Endpoint | `https://mcp.facebook.com/whatsapp_business_tools` |
| Authentication | Meta developer account sign-in (OAuth) |
| Status | Beta - interface and tool set may change |

## Prerequisites

The server acts on your own assets, so before an agent can use most tools you need:

- **Admin access to a business** you want to manage (the MANAGE role).
- **A WhatsApp-enabled app that you&#039;re an admin of**, connected to that business — an app with the WhatsApp Business Messaging use case (or the WhatsApp product on an older app). You must have admin access on the app, not only on the business.
- **Accepted WhatsApp Business Cloud API Terms of Service** for that business. Messaging and phone-number registration are blocked until an admin accepts them.

If any prerequisite is missing, the tool returns an actionable error that tells you what to fix and links to the page where you can fix it.

## Set up your IDE or agent client

The WhatsApp Business Tools MCP server is a **remote** server. Clients that support remote MCP servers can connect to the endpoint directly. For clients that only support local (stdio) servers, use the [`mcp-remote`](https://www.npmjs.com/package/mcp-remote) bridge, which proxies the remote server over stdio.

Configure your client using one of the sections below, then complete the [OAuth sign-in](#oauth-sign-in) to authenticate.

### Claude Code and Desktop

**Claude Code** — run the following command to add the server:

```bash
claude mcp add --transport http whatsapp_business_tools https://mcp.facebook.com/whatsapp_business_tools
```

Then run `/mcp` in a session and select **whatsapp_business_tools** to authenticate.

**Claude Desktop** — open **Settings** &gt; **Connectors** &gt; **Add custom connector**, then set:

- **Name**: `WhatsApp Business Tools`
- **URL**: `https://mcp.facebook.com/whatsapp_business_tools`

### OpenAI Codex App and ChatGPT

**Codex App** — open **Settings** (the gear icon, bottom left) &gt; **MCP Servers** &gt; **Add servers**, choose **Streamable HTTP**, then set:

- **Name**: `WhatsApp Business Tools`
- **URL**: `https://mcp.facebook.com/whatsapp_business_tools`
- **Authentication**: **OAuth**

Click **Authenticate** and complete the [OAuth sign-in](#oauth-sign-in).

**ChatGPT** — open **Settings** &gt; **Plugins** &gt; **Browse plugins**, click the **+** in the top right to add a new plugin, then set:

- **Name**: `WhatsApp Business Tools`
- **URL**: `https://mcp.facebook.com/whatsapp_business_tools`
- **Authentication**: **OAuth**

Save the plugin and complete the [OAuth sign-in](#oauth-sign-in).

## OAuth sign-in

The WhatsApp Business Tools MCP server authenticates with your Meta developer account over OAuth, so the same sign-in flow works with every client.

### Sign-in flow

After you add the server to your client (see [Set up your IDE or agent client](#set-up-your-ide-or-agent-client)):

1. **Start the connection.** Many clients begin the OAuth flow automatically when the server is added. Others require you to click **Connect** or **Authenticate** first.
2. **Sign in.** Your browser opens to sign in with your Meta account. If it doesn&#039;t open automatically, copy the link your client prints and open it manually.
3. **Grant access.** On the consent screen, select the businesses and WhatsApp-enabled apps you want the server to access.
4. **Confirm.** Return to your client and [verify your connection](#verify-your-connection).

You&#039;ll need to complete the sign-in flow again when you restart the client.

### Scopes

The server requests the following permissions during consent:

| Scope | Access |
|---|---|
| `business_management` | Discover and act on the businesses you administer that you selected during consent. |
| `whatsapp_business_management` | Manage WhatsApp Business accounts, phone numbers, message templates, and webhooks. |
| `whatsapp_business_messaging` | Send messages from your registered phone numbers. |

You can review or revoke access at any time at [facebook.com](https://www.facebook.com) &gt; **Settings** &gt; **Business Integrations**.

### Verify your connection

After adding the server, confirm your agent can see the tools:

1. Restart the IDE or agent client.
2. Ask the agent to list the tools it has from the `whatsapp_business_tools` server.
3. Run a low-risk read tool. For example, ask it to list the businesses you administer with `whatsapp_biz_businesses`.

If the agent reports no tools or an authentication error, re-check the endpoint URL and confirm you completed the sign-in flow.

## Tools

The WhatsApp Business Tools MCP exposes tools namespaced with the `whatsapp_biz_` prefix. Most tools are scoped to a business you administer, so a typical session starts by listing your businesses and picking one. The tool set is evolving during beta.

### Discovery

- **`whatsapp_biz_businesses`** — List the businesses you administer and that you granted during consent. Use this first to pick a business. *Example: &quot;List the businesses I can manage on WhatsApp.&quot;*
- **`whatsapp_biz_accounts`** — List the WhatsApp accounts (and messaging accounts) under a business. *Example: &quot;Show the WhatsApp accounts for business 1234567890.&quot;*
- **`whatsapp_biz_phone_numbers`** — List the phone numbers on a business or WhatsApp account, with status and onboarding state. *Example: &quot;List the phone numbers and their status for this business.&quot;*

### Phone number onboarding

- **`whatsapp_biz_add_phone_number`** — Add a phone number to a WhatsApp account. *Example: &quot;Add the phone number +1 555 010 1234 to my WhatsApp account.&quot;*
- **`whatsapp_biz_send_verification_code`** — Send a verification code to a phone number. *Example: &quot;Send a verification code by SMS to this number.&quot;*
- **`whatsapp_biz_verify_phone_number`** — Verify a phone number with the received code. *Example: &quot;Verify this number with code 123456.&quot;*
- **`whatsapp_biz_register_phone_number`** — Register a verified number so it can send and receive. *Example: &quot;Register this number for messaging.&quot;*

### Message templates

- **`whatsapp_biz_list_templates`** / **`whatsapp_biz_get_template`** — List and inspect message templates on an account. *Example: &quot;List the approved templates for this account.&quot;*
- **`whatsapp_biz_create_template`** / **`whatsapp_biz_update_template`** / **`whatsapp_biz_delete_template`** — Create, update, and delete message templates. *Example: &quot;Create a marketing template named order_update in en_US.&quot;*

### Messaging

- **`whatsapp_biz_send_message`** — Send a free-form text (only delivered inside the 24-hour customer service window) or an approved template message from a registered number. Confirms the target with you before sending. *Example: &quot;Send the order_update template to +1 555 010 9999 from this number.&quot;*

### Webhooks

- **`whatsapp_biz_configure_webhooks`** — Set the callback URL and verify token for the WhatsApp topic and subscribe the allowed fields. *Example: &quot;Configure webhooks for this business with callback https://example.com/webhook.&quot;*
- **`whatsapp_biz_subscribe_webhook`** — Subscribe a WhatsApp account to your app&#039;s webhooks so its events are delivered. *Example: &quot;Subscribe this WhatsApp account to webhooks.&quot;*

### Account setup

- **`whatsapp_biz_configure_payments`** — Configure a payment method so the account can send billable messages. *Example: &quot;Help me set up payments for this account.&quot;*
- **`whatsapp_biz_verify_business`** — Check and start business verification. *Example: &quot;What&#039;s the business verification status, and how do I start it?&quot;*
- **`whatsapp_biz_system_user_token`** — Get a link to generate a **system user access token** for your business, so you can build a custom messaging solution that calls the WhatsApp Cloud API directly. Token generation happens securely in Business Settings — the tool returns a deep link to the **System Users** page, where you select a system user and generate the token. To learn more, see [Create a system user and generate a permanent access token](https://developers.facebook.com/documentation/business-messaging/whatsapp/get-started#step-5-create-a-system-user-and-generate-a-permanent-access-token). *Example: &quot;Give me a link to create a system user access token for this business.&quot;*

## Rate limits

Requests are rate limited per user per tool to protect the platform. If you exceed the limit, the tool returns a rate-limit error; retry after a short wait.

## Feedback

If you have feedback or want to see more tools, [share your feedback](https://docs.google.com/forms/d/e/1FAIpQLSdy5WTAzvSOYx0rBkPC8sDIjkUyOMR09MsQwKLQwB48zc_MEg/viewform).

## Related

- [MCP overview](https://developers.facebook.com/documentation/mcp)
- [WhatsApp Business Platform documentation](https://developers.facebook.com/documentation/business-messaging/whatsapp)


---

Full documentation index for this product: https://developers.facebook.com/documentation/mcp/llms.txt
