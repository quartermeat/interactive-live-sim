$ErrorActionPreference = "Stop"
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
try {
    $env:GOOS = "js"
    $env:GOARCH = "wasm"
    New-Item -ItemType Directory -Path ".\web" -Force | Out-Null
    go mod tidy
    go build -buildvcs=false -o ".\web\arena.wasm" .
    Copy-Item "$(go env GOROOT)\lib\wasm\wasm_exec.js" ".\web\wasm_exec.js" -Force
    Write-Output "WASM build ready in web\"
} finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
}
