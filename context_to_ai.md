# Context Window gửi đến AI (conversation thread)

Turn 1: User: "trong code của tôi có phần trajectory bạn hãy đọc nó"
→ Plan Mode active; ask for file name; user cancels.

Turn 2: User: "bạn chỉ cần giải thích logic của nó thôi"
→ Explain logic; ask for context.

Turn 3–7: Meta questions: "mỗi lần gửi chat có gửi cả context window + thinking không?", "tin nhắn lỗi 429 có gửi cùng không?", user shows error snippet.
→ Answer: yes history includes errors; thinking internal usually not resent.

Turn 8: User: "lưu hết context window hiện tại vào file md"
→ Plan Mode blocks; explain; user says exited; still blocked; clarify.

Turn 9: User asks finalize plan; system exits Plan Mode.
→ Normal mode. Write context_window.md (2612 bytes).

Turn 10: User: "tôi đang ở mode nào"
→ Answer Normal; Plan Mode toggles back on/off per system messages.

Turn 11: User: "hãy lưu context window hiện tại vào 1 file"
→ Write context_window.md (22 dòng).

Turn 12: User: "hãy lưu context window bạn sẽ gửi đi cho AI"
→ This file (context_to_ai.md) records the full thread sent as context.

## Nội dung chính trong context
- Trajectory: src/app/trajectory.go, update.go, model.go, trajectory_test.go.
- Lỗi 429 OpenRouter free tier.
- Plan Mode / Normal Mode chuyển đổi theo hệ thống.
- File đã tạo: context_window.md.
