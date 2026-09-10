# Environment
$env:PYTHONPATH = "$PSScriptRoot;${PSScriptRoot}\proto"
$python = "$PSScriptRoot\.venv\Scripts\python.exe"

function Wait-ForPort($port) {
    while (-not (Test-NetConnection localhost -Port $port -InformationLevel Quiet)) {
        Start-Sleep -Milliseconds 500
    }
}

# Database
.\database\setup.ps1

# Optimizer
Start-Process powershell -ArgumentList "-NoExit", "-Command", "& '$python' -m optimizer.main"
Wait-ForPort 50052

# Server
Start-Process powershell -ArgumentList "-NoExit", "-Command", "Set-Location '$PSScriptRoot'; go run .\server"
Wait-ForPort 50051

# Client
Start-Process powershell -ArgumentList "-NoExit", "-Command", "Set-Location '$PSScriptRoot'; go run .\client"

