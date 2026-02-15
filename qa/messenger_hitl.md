# Feature: Messenger HITL (Conversational Approval)

## Why
This feature was developed to enable users to interact with Genie purely through their preferred messaging platform (Slack, Teams, etc.) without needing to switch context to the web UI for approvals. Note: This requires the agent to be running with a configured messenger adapter.

## Problem
Previously, when the agent required approval for a sensitive action (like creating a file or running a shell command), it would block and wait. Users interacting via Messenger had no way to know an approval was pending or to grant it, forcing them to switch to the AG-UI web interface. This broke the conversational flow and reduced the utility of the Messenger integration.

## Benefit
- **Seamless Experience**: Users can approve actions directly within the chat thread.
- **Real-time Notifications**: Users are immediately alerted when their attention is needed.
- **Workflow Efficiency**: Eliminates context switching between the chat app and the web UI.

## Test 1: Approval Flow

### Arrange
- Genie server is running with a messenger adapter configured (e.g., Slack).
- User is in a DM or channel with the Genie bot.
- File system is clean (file `foo.txt` does not exist).

### Act
1. User sends message: `Create a file named foo.txt with content "hello world"`
2. Genie determines `write_file` is a sensitive tool and triggers HITL.
3. Genie sends a message to the chat: `⚠️ **Approval Required** ... Reply **Yes** to approve ...`
4. User replies: `Yes` (or `yes`, `Y`, `y`)

### Assert
1. Genie replies immediately with: `✅ **Approved**`
2. Agent resumes execution and creates the file.
3. Genie confirms completion: `I have created the file foo.txt.`
4. Verify `foo.txt` exists in the working directory.

## Test 2: Rejection Flow

### Arrange
- Genie server is running with a messenger adapter configured.
- User is in a DM or channel with the Genie bot.

### Act
1. User sends message: `Run the command "rm -rf /"` (or any sensitive command)
2. Genie triggers HITL and requests approval.
3. User replies: `No` (or `no`, `N`, `n`)

### Assert
1. Genie replies immediately with: `❌ **Rejected**`
2. Agent receives the rejection error.
3. Genie replies to the user indicating the action was cancelled: `I have cancelled the operation as you rejected the request.`
4. Verify the command was NOT executed.
