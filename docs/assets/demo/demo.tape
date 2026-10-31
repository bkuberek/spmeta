# Recorded by record.sh inside the demo container; see ../../README.md.
# The container mounts docs/assets at /out.
Output "/out/demo.gif"

Set Shell bash
Set FontFamily "DejaVu Sans Mono"
Set FontSize 22
Set Width 1100
Set Height 380
Set Padding 28
Set TypingSpeed 45ms
Set Framerate 20
Set CursorBlink false
Set Theme {"name":"spmeta","black":"#111716","red":"#E07A6B","green":"#8FBF9F","yellow":"#F4C76B","blue":"#7FA8C9","magenta":"#C79BB8","cyan":"#7FBFB5","white":"#F1F2EB","brightBlack":"#55615A","brightRed":"#E07A6B","brightGreen":"#8FBF9F","brightYellow":"#F4C76B","brightBlue":"#7FA8C9","brightMagenta":"#C79BB8","brightCyan":"#7FBFB5","brightWhite":"#F1F2EB","background":"#111716","foreground":"#F1F2EB","selection":"#303A36","cursor":"#F4C76B"}

Hide
Type "export PS1='$ ' && set +m && clear"
Enter
Sleep 500ms
Show

# One line of text.
Type "spmeta"
Sleep 400ms
Enter
Sleep 2.5s
Ctrl+L

# JSON for scripts.
Type "spmeta --format json | jq '{state, title, artist, album}'"
Sleep 400ms
Enter
Sleep 3s
Ctrl+L

# Your own template.
Type "spmeta --template '{state_icon} {artist} — {title}'"
Sleep 400ms
Enter
Sleep 2.5s
Ctrl+L

# Watch mode: the player skips to the next track, then pauses.
Hide
Type "(sleep 3.5; echo next > $DEMO_TRIGGER; sleep 3; echo pause > $DEMO_TRIGGER) &"
Enter
Type "clear"
Enter
Sleep 300ms
Show
Type "spmeta --watch"
Sleep 400ms
Enter
Sleep 9s
Ctrl+C
Sleep 1s
