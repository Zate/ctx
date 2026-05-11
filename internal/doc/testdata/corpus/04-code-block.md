# Code Blocks

## Backtick Fence

Standard triple-backtick code block:

```go
package main

import "fmt"

func main() {
    fmt.Println("hello, world")
}
```

## Tilde Fence

Nested code block using tilde fences (allows backticks inside):

~~~markdown
Here is a code block inside a tilde fence:

```go
func nested() {}
```
~~~

## Indented Code

    indented code block
    second line

## Inline Code

Text with `inline code` and more text.
