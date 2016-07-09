
$revision = git rev-parse --verify --short HEAD

if ($?) {
    write "Revision: '$revision'"
} else {
    Write-Error "Could not parse git revision"
    write $revision
    exit
}

$ENV:GOOS = 'linux'
$ENV:GOARCH = 'arm'

go build -ldflags "-X github.com/verath/owbot-bot/lib/constants.REVISION=$revision" github.com/verath/owbot-bot

$ENV:GOOS = $null
$ENV:GOARCH = $null