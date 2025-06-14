# WebSocket Server

A high-performance WebSocket server written in Go built to integrate with any ui and api.
Super easy to extend or customize.

## Features

- REST endpoint for sending messages to specific users or broadcasting to teams
- WebSocket connections for real-time message delivery
- Team and user scoping for message routing
- Heartbeat mechanism to ensure connections stay alive

## Requirements

- Go 1.21.5 or later

## Installation

\`\`\`bash
# Clone the repository
git clone <repository-url>
cd websocket-server

# Install dependencies
go mod download

# Build the server
go build -o websocket-server .
\`\`\`

## Running the Server

\`\`\`bash
# Run directly
./websocket-server

# Or with Docker
docker build -t websocket-server .
docker run -p 8080:8080 websocket-server
\`\`\`

## API Documentation

### WebSocket Connection

Connect to the WebSocket server:

\`\`\`
GET /ws?team_id=<team_id>&user_id=<user_id>
\`\`\`

Parameters:
- `team_id`: The team ID the user belongs to
- `user_id`: The user's ID

### Send Message Endpoint

Send a message to a specific user or broadcast to a team:

\`\`\`
POST /send
\`\`\`

Request Body:
\`\`\`json
{
  "team_id": "team123",
  "user_id": "sender456",
  "target_user_id": "recipient789",
  "message_type": "notification",
  "content": "Hello, this is a test message",
  "broadcast": false
}
\`\`\`

Parameters:
- `team_id`: The team ID
- `user_id`: The sender's user ID
- `target_user_id`: (Optional) The recipient's user ID
- `message_type`: Type of message
- `content`: Message content
- `broadcast`: If true, sends to all users in the team (ignores target_user_id)

Response:
\`\`\`json
{
  "success": true,
  "delivered": 1
}
