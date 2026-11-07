# Factory HTTP and UI

- U1 is read-only. Do not add desired-state mutations or simulator experiment controls.
- Serve embedded assets without external CDNs. Keep browser credentials out of responses.
- Source failures must produce partial overview data and explicit unknown states.
- Use textContent for source-provided strings; never interpolate them into innerHTML.
- Poll without overlapping requests and invalidate visible data when the API is unreachable.
- Keep UI labels in Russian and API fields, reason codes and logs in English.
