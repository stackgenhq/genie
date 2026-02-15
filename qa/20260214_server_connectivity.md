# Server & Connectivity — Acceptance Criteria

> Tests for server startup, chat.html connection, and error handling for invalid servers.

---

## 1 — Server Starts Successfully

### Why
The Genie server is the core entry point for all interactions. Verifying it starts correctly ensures the binary is built properly and the configuration is valid.

### Problem
A misconfigured or broken binary would silently fail, blocking all downstream testing and usage.

### Benefit
Gives confidence that the server is operational before running any other tests.

### Arrange
```bash
source .env
```

### Act
```bash
./build/genie grant --audit-log-path ./audit.log
```

### Assert
- Stdout shows: `🧞 Genie AG-UI server starting on :8080`
- `http://localhost:8080/health` returns HTTP 200

---

## 2 — Chat.html Connects to Server

### Why
The chat UI is the primary user-facing interface. A successful connection validates the AG-UI protocol handshake and CORS configuration.

### Problem
Without this, users would have no way to interact with Genie through the browser.

### Benefit
Confirms end-to-end connectivity from the browser to the running server.

### Arrange
- Server is running on port 8080 (Test 1)
- Open `docs/gh-pages/chat.html` in a browser

### Act
1. Verify the **Endpoint** field reads `http://localhost:8080`
2. Click **Connect**

### Assert
- The badge in the top-right changes to **● Connected** (green)
- A system message appears: `Connected to Genie at http://localhost:8080`

---

## 11 — Error Handling (Invalid Server)

### Why
Users may misconfigure the endpoint or the server may be down. The UI must handle this gracefully.

### Problem
Without error handling, the UI could crash or hang indefinitely on a bad connection.

### Benefit
Provides a clear, non-destructive failure mode so users know to check their server.

### Arrange
- Open `docs/gh-pages/chat.html` in a browser

### Act
1. Change the endpoint to `http://localhost:9999` (no server running)
2. Click **Connect**
3. Try sending a message

### Assert
- The badge does **NOT** show "Connected"
- An error message or connection failure is shown to the user
- The UI does not crash / remain in a broken state
