# Mid-Run Feedback (Working Memory Injection) Test Plan

This document outlines the steps to verify that a user can asynchronously provide feedback while the agent is running via AG-UI, and that the agent adapts to the feedback organically.

## Prerequisites

1.  Genie backend must be running locally:
    ```bash
    export AWS_PROFILE=<your_aws_profile>
    make go/tv
    go run ./main.go --log-level debug
    ```
2.  Open the local browser UI at: `./docs/chat.html`
3.  Connect to the active endpoint (usually `http://localhost:9876`).

## Test Case 1: Mid-Run Course Correction

**Goal:** Verify that the agent can read and adhere to new instructions supplied *after* a multi-step plan has started executing.

### Steps:

1.  In the chat input, type a complex multi-step prompt that takes some time to execute.
    *   *Prompt Example:* "Research the top 3 AI agent frameworks (LangGraph, AutoGen, CrewAI), write a short summary for each, and then translate the summary to French."
2.  Press **Enter** to start the stream.
3.  **Immediately while the agent is still thinking/executing its plan**, type the following into the chat input:
    *   *Mid-Run Feedback:* "Actually, forget French. Translate the final summary to Spanish instead."
4.  Press **Enter**.
    *   *Assertion 1:* The input box should clear, and the user message should appear in the chat history immediately, even though the agent is still working.
5.  Wait for the agent to finish its run.
    *   *Assertion 2:* The agent's final output should be in **Spanish**, proving that it read the injected working memory and organically course-corrected in the middle of its `reactree` plan.

## Test Case 2: Simple Interruption

**Goal:** Verify that a basic plan-step picks up the context.

### Steps:

1.  Type: "Write a 5 paragraph essay about the history of the internet. Take your time."
2.  Press **Enter**.
3.  While it says "Processing your request..." (or is emitting the first paragraph), quickly type: "Stop writing about the internet, write about cats instead."
4.  Press **Enter**.
5.  *Assertion:* The agent should realize the user updated the goal mid-stream, acknowledge the change, and seamlessly pivot to talking about cats in the subsequent output chunks.
