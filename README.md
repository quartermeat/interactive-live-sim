# Interactive Live Simulator

Local interactive LIVE app with a real stream scene, a control surface, and an optional offline emulator.

## Run

From PowerShell:

```powershell
Set-Location C:\Users\jerem\scratch\interactive-live-sim
python server.py
```

Open these URLs:

- http://localhost:8090/control — operator control surface
- http://localhost:8090/stream — clean vertical scene for OBS/TikTok LIVE Studio
- http://localhost:8090/stream-wasm — Go/WASM Canvas 2D version of the stream scene, rendered at native 1080×1980
- http://localhost:8090/emulator — optional standalone emulator

Add the `/stream` URL to OBS as a Browser Source at 1080x1920, or open it full-screen and capture the window. Keep `/control` on a separate monitor or browser tab.

If port 8090 is unavailable, run `$env:PORT='8091'; python server.py` and use the matching port.

Start the complete live setup from PowerShell with:

```powershell
cd D:\Codex\Projects\work\interactive-live-sim
.\scripts\start-live.ps1 -TikTokUsername quartermeat
```

This starts the connector, real TikTok LIVE adapter, arena server, and a monitor-fitted Chrome stream window. Use `-SkipAdapter` when testing without live TikTok data.

Build the Go/WASM Canvas renderer with:

```powershell
cd D:\Codex\Projects\work\interactive-live-sim
.\scripts\build-wasm.ps1
```

The browser shell keeps a native 1080×1980 backing canvas and scales it proportionally to the available browser window, so the complete scene remains visible on smaller monitors without clipping.

The arena also subscribes to the normalized connector at `http://127.0.0.1:8787/events`. Start that service in another PowerShell window:

```powershell
cd D:\Codex\Projects\work\tiktok-connector
.\scripts\run.ps1
```

Events sent from the connector console to `/api/events` will appear in the arena stream. The connector is currently a normalized/manual bridge; a direct TikTok LIVE adapter still needs to be attached to its `POST /api/events` endpoint.

The interface supports multiple named viewers, optional avatar URLs, viewer selection, comments, likes, follows, gifts, a random audience burst, and an inspectable event payload. The event payload is intentionally shaped for a future TikTok connector:

```json
{
  "type": "gift",
  "viewer": {
    "id": "...",
    "username": "NeonMoth",
    "avatar_url": null
  },
  "gift": "Rose"
}
```
