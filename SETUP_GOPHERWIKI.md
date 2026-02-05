# Setup GOPHERWIKI

## Option 1: Export it in your shell
```sh
export SECRET_KEY="your-random-secret-key-here-at-least-16-chars"
export REPOSITORY="/tmp/gopherwiki-test-repo"
mkdir -p $REPOSITORY
go run ./cmd/gopherwiki
```

## Option 2: Inline when running

```sh
SECRET_KEY="your-random-secret-key-here" REPOSITORY="/tmp/gopherwiki-test-repo" go
run ./cmd/gopherwiki
```

## Option 3: Generate a random one

```sh
export SECRET_KEY=$(openssl rand -hex 32)
```

The app also requires REPOSITORY to point to a directory (it will be initialized as a git repo if empty).