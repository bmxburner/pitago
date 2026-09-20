# tests/

Quy ước test của dự án (Go bắt buộc, không phải ý thích):

- **Unit test white-box** (`*_test.go` nằm cạnh code trong `src/...`): được truy
  cập field/method private (`m.blocks`, `m.pet`, `atToken`...). Go yêu cầu file
  test loại này **phải ở cùng folder với code** — dời đi chỗ khác là gãy build.
  Viết test mới cho logic nội bộ thì đặt cạnh file code, đúng chuẩn Go.
- **Integration test black-box** (`tests/integration/`): chỉ dùng API public
  (`app.New`, `Model.View`/`Update`, các package `src/components/*`). Dùng để
  kiểm tra các component ghép với nhau có chạy đúng không.

Chạy test:

```sh
go test ./src/app/ ./src/builtin/ ./src/pirpc/ ./tests/...
```

(Lưu ý: `src/stack` đang gãy build từ trước — sai API bubbletea/glamour,
không liên quan tới cấu trúc này, xem `go vet ./src/stack/`.)
