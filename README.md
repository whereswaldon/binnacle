# binnacle

Compose PromQL queries and get results fast!

![screenshot](https://github.com/whereswaldon/binnacle/raw/main/img/binnacle.png)

## Features

- query auto-formatting (wip)
- rapid feedback errors and warnings about the query being composed
- vector result visualization

## Planned features

- matrix/scalar result visualization
- query macros for easier composition


## Usage

Install [Gio's dependencies](https://gioui.org/doc/install) for your OS.

Then
```
export PROM_TOKEN="<token>"
go run . --addr <http(s) address of your prometheus instance>
```

## License

Dual Unlicense/MIT
