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
{
	"RoomCount": 1,
	"Rooms": [
		{
			"Server": {
				"Name": "[RUS] maksvalmont (r16020)",
				"IP": "37.139.84.48",
				"Port": 56789,
				"ServerType": "mstDedicated",
				"OS": "Windows",
				"Ping": 76
			},
			"GameRevision": 16020,
			"RoomID": 1,
			"OnlyRoom": false,
			"GameInfo": {
				"GameState": "mgsGame",
				"PasswordLocked": false,
				"PlayerCount": 8,
				"GameOptions": {
					"Peacetime": 60,
					"SpeedPT": 1.3,
					"SpeedAfterPT": 1,
					"RandomSeed": 1508940013,
					"MissionDifficulty": "mdNone"
				},
				"Players": [
					{
						"Name": "ORDAORKI",
						"Color": "#FB8FDE",
						"Connected": true,
						"LangCode": "rus",
						"Team": 1,
						"IsSpectator": false,
						"IsHost": true,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "[E*] Progruz",
						"Color": "#64CEFA",
						"Connected": true,
						"LangCode": "rus",
						"Team": 2,
						"IsSpectator": false,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "POLAKO",
						"Color": "#004B13",
						"Connected": true,
						"LangCode": "pol",
						"Team": 0,
						"IsSpectator": true,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "Igor{ORKI}",
						"Color": "#1B1B1B",
						"Connected": true,
						"LangCode": "rus",
						"Team": 0,
						"IsSpectator": true,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "Toxic_Ghoul",
						"Color": "#FF07FF",
						"Connected": true,
						"LangCode": "rus",
						"Team": 2,
						"IsSpectator": false,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "Regus",
						"Color": "#A2C50E",
						"Connected": true,
						"LangCode": "pol",
						"Team": 0,
						"IsSpectator": true,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "Das Boot",
						"Color": "#EB0000",
						"Connected": true,
						"LangCode": "eng",
						"Team": 0,
						"IsSpectator": true,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					},
					{
						"Name": "ooga booga-ORKI",
						"Color": "#F86C07",
						"Connected": true,
						"LangCode": "slv",
						"Team": 1,
						"IsSpectator": false,
						"IsHost": false,
						"PlayerType": "nptHuman",
						"WonOrLost": "wolNone"
					}
				],
				"Description": "2x2 gogogo",
				"Map": "CiW 2x2",
				"GameTime": "00:46:44"
			}
		}
	]
}
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
