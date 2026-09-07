function Wait-ForPort($port) {
    while (-not (Test-NetConnection localhost -Port $port -InformationLevel Quiet)) {
        Start-Sleep -Milliseconds 500
    }
}

# Optimizer
Start-Process powershell -ArgumentList "-NoExit", "-Command", "`$env:PYTHONPATH='.;proto'; .\.venv\Scripts\python.exe -m optimizer.main"
Wait-ForPort 50052

# Server
Start-Process powershell -ArgumentList "-NoExit", "-Command", "go run .\server"
Wait-ForPort 50051

# Client
Start-Process powershell -ArgumentList "-NoExit", "-Command", "go run .\client"

