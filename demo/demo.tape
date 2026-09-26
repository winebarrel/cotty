# Generate demo.gif with: cd demo && vhs demo.tape
# Needs go, tmux, python3 and vhs.
Output demo.gif
Set Shell bash
Set FontSize 16
Set Width 1600
Set Height 820
Set Padding 12
Set Framerate 20
Set Theme "Dracula"
Hide
Type "./start.sh && tmux -L cottydemo attach -t demo"
Enter
Sleep 3s
Show
Wait+Screen@120s /plain text under/
Sleep 4s
