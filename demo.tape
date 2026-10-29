# Records docs/demo.gif from demo mode (fictional data, changes nothing).
#   vhs demo.tape
Output docs/demo.gif

Require go

Set Shell "bash"
Set FontSize 15
Set Width 1100
Set Height 860
Set Padding 16
Set TypingSpeed 60ms

Hide
Type "go build -o /tmp/serve-a-port ./cmd/serve-a-port && alias serve-a-port=/tmp/serve-a-port && clear"
Enter
Sleep 5s
Show

Type "serve-a-port demo"
Sleep 500ms
Enter
Sleep 4s

# Repair the stale forward (second row).
Down
Sleep 800ms
Type "r"
Sleep 1.5s
Enter
Sleep 3.5s
Enter
Sleep 1s

# Share storybook, picked from the running servers.
Type "a"
Sleep 1.2s
Enter
Sleep 1s
Type "x"
Sleep 600ms
Enter
Sleep 1.5s
Enter
Sleep 3.5s
Enter
Sleep 1s

# Show the QR code for the first forward.
Up
Enter
Sleep 3s
Escape
Sleep 800ms

# Quit, keeping the forwards.
Type "q"
Sleep 1.5s
Enter
Sleep 500ms
