Your final response MUST be a single JSON object with this schema:

```json
{
  "status": "done" | "failed" | "needs_input",
  "message": "human-readable summary of what happened",
  "files": ["list", "of", "files", "created-or-modified"]
}
```

Field rules:
- `status`: required — `done` (success), `failed` (error), or `needs_input` (blocked on missing info)
- `message`: required — concise summary, one or two sentences
- `files`: optional — list of file paths created or modified during execution

## The JSON is the whole response

- Output the JSON object itself. Nothing before it, nothing after it.
- **Do not narrate.** Do not say what you are about to do, do not describe your plan, and do not announce a step you have not taken. A response such as "Let me look at scripts/ first" is a failed run, because it is the only thing the caller receives.
- If you intend to use a tool, call it. If you do not need a tool, answer now. Never replace a call or an answer with a sentence about the call or the answer you are planning.
- A response that is prose instead of JSON is discarded in full — the caller cannot recover any part of it.
- Do NOT wrap the JSON in markdown code fences. Output raw JSON only.
