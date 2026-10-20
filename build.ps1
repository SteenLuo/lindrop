param([ValidateSet('386','amd64')][string]$Architecture='386')
$ErrorActionPreference='Stop'
Set-Location -LiteralPath $PSScriptRoot
if ((& go version) -notmatch 'go1\.20\.') { throw '请使用 Go 1.20.x 保留 Win7 编译目标支持。' }
$env:GOOS='windows';$env:GOARCH=$Architecture;$env:CGO_ENABLED='0'
& go test -mod=vendor -v -timeout 150s ./...
if ($LASTEXITCODE -ne 0) { throw '测试失败' }
& go build -mod=vendor -trimpath -ldflags '-H windowsgui -s -w' -o '邻传.exe' .
if ($LASTEXITCODE -ne 0) { throw '构建失败' }
Write-Output '已生成独立客户端 邻传.exe'
