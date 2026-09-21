# StreamTanks Build, Test & Lint Script
[CmdletBinding()]
param(
    [switch]$SkipFrontend,
    [switch]$SkipTest,
    [switch]$SkipLint,
    [switch]$FixDiff,
    [switch]$Fix
)

$ErrorActionPreference = "Stop"

function Write-Step ($msg) {
    Write-Host "`n>>> $msg" -ForegroundColor Cyan
}

function Write-Success ($msg) {
    Write-Host "[SUCCESS] $msg" -ForegroundColor Green
}

function Write-Fail ($msg) {
    Write-Host "[FAILED] $msg" -ForegroundColor Red
}

try {
    # Ensure native GOOS / GOARCH environment (in case deploy scripts set GOOS=linux)
    Remove-Item env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item env:\GOARCH -ErrorAction SilentlyContinue

    # 1. Test & Build TypeScript Frontend
    if (-not $SkipFrontend) {
        Write-Step "Testing TypeScript frontend..."
        & npm.cmd run test
        if ($LASTEXITCODE -ne 0) { throw "Frontend tests failed." }
        Write-Success "Frontend tests passed."

        Write-Step "Building TypeScript frontend..."
        & npm.cmd run build:prod
        if ($LASTEXITCODE -ne 0) { throw "Frontend build failed." }
        Write-Success "Frontend built successfully."
    }

    # 2. Go Modernization (Optional checks)
    if ($FixDiff) {
        Write-Step "Inspecting Go modernization diffs (go fix -diff ./...)..."
        & go fix -diff ./...
        Write-Success "Go fix diff check finished."
    }
    if ($Fix) {
        Write-Step "Applying Go modernizations (go fix ./...)..."
        & go fix ./...
        if ($LASTEXITCODE -ne 0) { throw "go fix failed." }
        Write-Success "Go fix applied."
    }

    # 3. Run Go Tests
    if (-not $SkipTest) {
        Write-Step "Running Go test suite..."
        & go test -v ./...
        if ($LASTEXITCODE -ne 0) { throw "Go test suite failed." }
        Write-Success "All Go tests passed."
    }

    # 3. Run Linter
    if (-not $SkipLint) {
        Write-Step "Running Go linter..."
        $linter = Get-Command "golangci-lint-v2" -ErrorAction SilentlyContinue
        if (-not $linter) {
            $linter = Get-Command "golangci-lint" -ErrorAction SilentlyContinue
        }
        if ($linter) {
            & $linter.Source run ./...
            if ($LASTEXITCODE -ne 0) { throw "Linter found issues." }
            Write-Success "Linter passed cleanly."
        } else {
            Write-Host "Warning: No golangci-lint or golangci-lint-v2 found on PATH; skipping lint step." -ForegroundColor Yellow
        }
    }

    # 4. Build Go Binary
    Write-Step "Compiling StreamTanks binary..."
    & go build -o streamtanks.exe ./cmd/streamtanks
    if ($LASTEXITCODE -ne 0) { throw "Go build failed." }
    Write-Success "StreamTanks binary compiled: streamtanks.exe"

    Write-Host "`n==================================================" -ForegroundColor Green
    Write-Host " All build, test, and lint steps completed cleanly! " -ForegroundColor Green
    Write-Host "==================================================" -ForegroundColor Green
}
catch {
    Write-Fail $_.Exception.Message
    exit 1
}
