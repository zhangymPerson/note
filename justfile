# https://just.systems

default:
    echo 'Hello, world!'

format:
    go fmt ./...
    npx --yes --registry https://registry.npmmirror.com prettier --write --list-different .
