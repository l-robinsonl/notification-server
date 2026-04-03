# Notification Server

A Go realtime notification server for team-scoped and user-scoped delivery over WebSockets, with a REST entrypoint for backend-triggered notifications.

## Features

- REST delivery to a single user, a team, or all connected teams
- WebSocket connections with authenticated session setup
- Ping/pong heartbeat handling for stale connection cleanup

## Requirements

- Go 1.21 or later

## Running

```bash
go run ./src
```

By default the server loads `local_settings.yaml`. Override that path with:

```bash
CONFIG_PATH=/path/to/settings.yaml go run ./src
```

## HTTP API

### `POST /send`

Headers:

- `X-API-Key: <api key>`
- `Content-Type: application/json`

Request body:

```json
{
  "notification_id": "notif-123",
  "target_team_id": "team-123",
  "target_user_id": "user-456",
  "sender_user_id": "system",
  "message_type": "system_alert",
  "body": "Hello from the backend",
  "broadcast": false
}
```

Rules:

- `broadcast: false` sends to `target_user_id`.
- `broadcast: false` with `target_team_id` sends to that user in that team only.
- `broadcast: false` without `target_team_id` sends to every connected session for that user across all teams.
- `broadcast: true` with `target_team_id` broadcasts to all connected users in that team.
- `broadcast: true` without `target_team_id` broadcasts to all connected users in all teams.

Response:

```json
{
  "success": true,
  "delivered": 1
}
```

### `GET /health`

Returns basic hub health:

```json
{
  "status": "healthy",
  "message": "WebSocket server is running",
  "total_teams": 2,
  "total_clients": 8
}
```

## WebSocket API

### Connect

Open a websocket to:

```text
GET /ws
```

The first websocket message must be an auth payload:

```json
{
  "type": "auth",
  "teamId": "team-123",
  "token": "<jwt>"
}
```

The backend auth response for that JWT must include the user's current `selectedTeam`, and it must match the requested `teamId`. The server accepts either:

- `settings: { "selectedTeam": "team-123" }`
- `selectedTeam: "team-123"`

For local development with fake auth enabled, use `token: "fake_development_token"` and include `userId`.

Successful auth response:

```json
{
  "type": "authSuccess",
  "message": "Successfully authenticated"
}
```

Auth failure response:

```json
{
  "type": "auth_error",
  "message": "invalid JWT token provided"
}
```

After authentication, clients do not send application messages. The server only pushes notification payloads to authenticated sockets.

Delivered notification payload:

```json
{
  "notificationId": "notif-123",
  "targetTeamId": "team-123",
  "targetUserId": "user-456",
  "senderUserId": "system",
  "messageType": "system_alert",
  "body": "Hello from the backend",
  "timestamp": 1775237123456
}
```
