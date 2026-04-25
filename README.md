# ServerPollerJson
Console Application for KaM Remake (net protocol R16000). Polls servers status and returns detailed JSON.\
Based on original from reyandme/kam_remake ..\Utils\Server Poller\


# Format JSON
- root: `RoomCount`, `Rooms`
- room: `Server`, `GameRevision`, `RoomID`, `OnlyRoom`, `GameInfo`
- server: `Name`, `IP`, `Port`, `ServerType`, `OS`, `Ping`
- game info: `GameState`, `PasswordLocked`, `PlayerCount`, `GameOptions`, `Players`, `Description`, `Map`, `GameTime`
- game options: `Peacetime`, `SpeedPT`, `SpeedAfterPT`, `RandomSeed`, `MissionDifficulty`
- player: `Name`, `Color`, `Connected`, `LangCode`, `Team`, `IsSpectator`, `IsHost`, `PlayerType`, `WonOrLost`
`Color` is returned as a regular RGB hex string: `#RRGGBB`.

Example:
```json

```

Build:

```
go build
```

Run:

```powershell
.\ServerPollerJson.exe -timeout 6s -master http://master.kamremake.com/ -gameRevision r16020
```

Options:

- `-master`: master server URL, default `http://master.kamremake.com/`
- `-timeout`: total polling timeout, default `6s`
- `-gameRevision`: game revision sent to master server as `coderev`, default `r16020`
- `-includeEmptyRooms`: include rooms without players, default `false`
