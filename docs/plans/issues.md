1. can we add a 1 line padding above status bar (idle/streaming)
2. remove top & bottom padding for inputbox, just keep it two line input box that still grows with content and shift + enter creates new line grows the input bar
3. when there are multiple agents running, if I ended up in a subagent active state at the time of subagent work done, the original primary loop isn't visible to pick and go back to (I asked for to show only non-idle loops for subagents but the implementation may have been done in a way to hide non-active loops that are idle, so even primary loop getting hidden if I'm not actively in it; give exception to primary loop to show always)
4. can you remove gap only between thinking block and following AIMessage
5. make all sec as s for exmple instead of 23sec just show 23s
6. for status bar we are showing number of sec turn took while turn is active but when turn is done; can we put a msg (may be a harness message type) that looks like hollow circle indent and says turn ran for 25s in light grey that is outfocus.
7. when popups for gates popped up it showed only +3 pending when there were about 8 pending; happened multiple times, can you check the logic; may be because each of these coming from different loops
8. when multiple parallel tool calls made - remove line padding in between tool calls and tool call and AI Message; so it looks cohesively that AI Message lead this tool call
9. add a line after thinking/thought for xsec as padding between thinking text
10. for messages like "Subagent(operator)  "Perform a focused security audit of sandbox confine" the text isn't wrapping according to screen width rather its going out of the width of the screen
11. ▌ queued  how long will you take? make QUEUED appear on top of message in our blue #A2D2FF color
