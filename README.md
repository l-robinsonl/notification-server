# WebSocket Notification Server

(Will be) A production-grade, enterprise-ready WebSocket notification server written in Go. Features JWT authentication, team-based message routing, and seamless integration with existing APIs and frontends. Currently only REST->notification-server->WebSockets workflow has been tested. WebSocket->WebSocket has not.  

## Features

### Core Functionality
- **JWT Authentication** - Secure token validation with your existing backend
- **Team-based Message Routing** - Isolated notifications per team/organization
- **Real-time WebSocket Connections** - Instant message delivery without polling
- **REST API for Message Sending** - Easy integration with existing backend services
- **Auto-reconnection & Team Switching** - Seamless user experience when changing contexts

### Enterprise Ready
- **Connection Management** - Automatic cleanup and heartbeat monitoring  
- **Scalable Architecture** - Hub-based message distribution
- **Production Logging** - Comprehensive debug and monitoring capabilities
- **CORS Support** - Ready for cross-origin frontend integration
- **Graceful Error Handling** - Robust connection failure recovery

## Requirements

- Go 1.21+ 
- Access to your authentication backend API

## Quick Start

```bash
# Clone and setup
git clone <repository-url>
cd notification-server/src

# Install dependencies  
go mod download

# Run the server
PORT=8081 go run ./

# Server starts on http://localhost:8081