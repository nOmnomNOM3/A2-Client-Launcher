# Launcher Settings screen

Settings button → two tabs. Arrows cycle values; changed values turn gold; SAVE writes, Back/Esc discards, Undo resets, "Edit ini" opens ddraw.ini in Notepad.
Before writing, `ddraw.ini`, `allods2.cfg` and the registry values are copied to `<game>\.a2launcher\backup\settings-<time>\`.
Files are edited line by line: comments, unknown keys and other sections stay byte-identical (CRLF kept). A setting whose current value is not one of the presets shows "Custom" and is never touched unless you pick a value.

## Where each setting lives (verified)
| Store | Source of truth | Keys |
|---|---|---|
| `ddraw.ini` `[ddraw]` (also `[allods2]` if that override section exists) | cnc-ddraw 7.1.0.0 (git~541b5de) shipped with the client; keys from its own ddraw.ini | windowed, fullscreen, width, height, maintas, boxing, resizable, devmode, toggle_borderless, toggle_upscaled, keytogglefullscreen, renderer, shader, d3d9_filter, vsync, maxfps, singlecpu, keyscreenshot |
| `allods2.cfg` (game folder) | a2mgr.dll `ParseConfig2x` (config.cpp); names confirmed in the shipped dll | r_width+r_height (both needed), g_scrollspeed (0-100), r_fps, r_time, r_grid, v_screentype, z_language |
| Registry REG_DWORD | allods2.exe: `HKLM\SOFTWARE\1C\Allods 2` (32-bit). Without admin rights Windows uses `HKCU\Software\Classes\VirtualStore\MACHINE\SOFTWARE\WOW6432Node\1C\Allods 2` — that copy is edited (and HKLM too when writable) | GameSpeed, MusicEnabled, Shadows, Lighting, Smoothing, Animation, ShowAllHitPoints, ShowFlyingHP, ClanNames, MessageColors, Acknowledgement, TipsMode |

## Not exposed, and why
- Sound / music / speech volume (`SoundMusPos`, `SoundSfxPos`, `SoundSpeechPos`): stored as negative numbers (e.g. -700), likely DirectSound attenuation, scale not verified. Use the in-game sliders.
- AutoCasting (bit mask), FormationMode, WimpyMode, hatprogip/login/etc.: meaning or effect not verified, or managed by the Hat/launcher.
- In-game key bindings: the game has none in files. a2mgr supports `alias:<key> <action>` lines in allods2.cfg (kept untouched); a remap editor could come later.
- cnc-ddraw compatibility keys (hook, nonexclusive, fixchilds, ...): left alone; "Edit ini" for experts.
- Game speed only matters offline; online the server's speed is used. Range 0-8 follows the in-game slider (values 4 and 8 seen on a real install).

## Notes
- cnc-ddraw `savesettings=1` saves window position/size/mode when the game exits (e.g. after Alt+Enter); the screen shows what is in the file.
- "Game view size" shows more map at bigger sizes (fairness: agree a cap among players).
- Registry options appear only after the game has been started once.
