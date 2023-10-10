go build -ldflags="-s -w"
set zipfile=gmqtt_windows.zip
del %zipfile%
7z a -tzip %zipfile% gmqtt.exe config.yml gmqtt_*.bat
pause
