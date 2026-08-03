# ZoeyDCBot — Plan

Discord bot: respon cepat, AI-based, arsitektur gateway meniru Antares
(`/home/ubuntu/antares/internal/gateway/discord.go`).

## Asumsi (konfirmasi sebelum eksekusi)

1. **Bahasa: Go 1.26** — sama kayak Antares, udah tersedia di mesin, startup & latensi rendah.
2. **Gateway: pakai `discordgo`** (library mature, handle reconnect/heartbeat/resume). Pipeline & model concurrency-nya yang ditiru dari Antares (typing → async worker → streaming reply). *Varian B kalau mau zero-dep: salin `wsutil` + loop opcode dari Antares — lebih banyak kerja, kontrol penuh.*
3. **AI backend: OpenAI-compatible streaming endpoint**, dikonfigurasi via env (`LLM_BASE_URL` + `LLM_API_KEY` + `LLM_MODEL`) — bisa OpenAI, DeepSeek, vLLM lokal, Ollama, dsb. MVP standalone; opsi lanjutan: route ke agent Antares (pakai Manager/sessions).
4. **Input eksternal (prereq dari user):**
   - Bot token dari Discord Developer Portal (enable **Message Content Intent**)
   - Invite bot ke test server
   - API key LLM

## Steps (berurutan)

1. **Scaffold** — `go mod init` di project ini, add `discordgo` + loader env minimal. Verify `go build` hijau. *(coder)*
2. **Config** — `.env` + `.gitignore`: `DISCORD_BOT_TOKEN`, `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, `SYSTEM_PROMPT`, `ALLOWED_CHANNEL_IDS` (opsional), `CONTEXT_TURNS`. Validasi fail-fast kalau token/key kosong.
3. **Gateway core** — session discordgo: intents `GUILD_MESSAGES|MESSAGE_CONTENT|DIRECT_MESSAGES`, `Ready` handler, graceful shutdown (SIGINT/SIGTERM → close session).
4. **Slash commands** — register via REST: `/ping` (RTT gateway + LLM TTFT), `/clear` (reset konteks channel), `/help`.
5. **Message pipeline** — `onMessageCreate`: filter (DM ke bot, atau message yang mention bot di channel); langsung kirim **typing indicator**; enqueue ke worker pool (buffered channel + N workers) supaya loop gateway gak pernah keblok oleh LLM lambat.
6. **AI client** — OpenAI-compatible *streaming* chat completions: system prompt, rolling context (N turn terakhir per channel, in-memory + TTL), timeout 30s, retry 1x saat 5xx, parse SSE (`data:` lines).
7. **Streaming reply** — kirim message pertama secepat mungkin (first token), terus **edit** dengan chunk berikutnya (~1 edit/detik biar aman dari rate-limit); fallback: kalau kesamber 429, kirim message baru.
8. **Instrumentasi** — log TTFT & total latency per message; `/ping` laporin angka. Target: TTFT < 1–2 dtk (di luar overhead provider).
9. **Test & build** — unit test AI client pakai `httptest` mock SSE, test filter message; `go vet` + `go build`; **manual test di server Discord**: ukur latency, cek streaming, cek typing muncul cepat. *(coder + reviewer)*
10. **Deploy** — build binary + systemd unit `zoeydcbot.service` (restart on failure, log ke file), `.env` terpisah, gak pernah di-commit.

## Risiko / yang susah dibalikin — perhatian ekstra

- **Bot token & LLM key**: secret, jangan pernah di-commit; kalau bocor → regenerate di portal.
- **Message Content Intent**: wajib di-enable manual di portal; untuk >100 server butuh verification Discord.
- **Edit rate-limit**: streaming edit maksimal ~1/detik; kalau model cepet banget, kirim message baru lebih aman daripada edit.
- **Varian B (raw WS)**: mengganti `discordgo` ke `wsutil`-style buatan sendiri itu kerja tambahan 2–3 hari dan rawan bug reconnect — keputusan ini dicek dulu sebelum dikerjain.

## Definition of done

- Bot online di test server, `/ping` respond < 1 dtk
- Mention/DM → typing langsung muncul, jawaban keluar streaming
- Test hijau, `go vet` bersih, jalan sebagai systemd service

## Next action

User: bikin bot + token di Discord Developer Portal, enable Message Content Intent, invite ke test server, sediakan LLM key. Setelah itu eksekusi dimulai dari step 1–3.
