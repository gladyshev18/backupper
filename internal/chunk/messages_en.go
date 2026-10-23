package chunk

// enMessages translates the plan explanations for the English interface.
var enMessages = map[string]string{
	"Свободной памяти мало (%s), поэтому куски буферизуются во временных файлах на диске.": "Free memory is low (%s), so parts are buffered in temporary files on disk.",
	"Размер задан вручную: %s.": "Size set manually: %s.",
	"Два куска по %s займут больше половины свободного места на диске (%s).":                                                   "Two parts of %s would take more than half of the free disk space (%s).",
	"Два куска по %s займут больше %d%% свободной памяти (%s). Сервер может начать свопиться или агент будет убит OOM-killer.": "Two parts of %s would take more than %d%% of free memory (%s). The server may start swapping or the OOM killer may stop the agent.",
	"По диску: до %s на кусок (половина свободного места %s, два куска одновременно).":                                         "By disk: up to %s per part (half of the free space %s, two parts at a time).",
	"По памяти: до %s на кусок (%d%% от %s свободной RAM за вычетом резерва %s, два куска одновременно).":                      "By memory: up to %s per part (%d%% of %s free RAM minus a %s reserve, two parts at a time).",
	"По сети: ~%s за %d с при измеренной скорости %s/с — при обрыве повторно отправится немного.":                              "By network: ~%s in %d s at the measured %s/s — little has to be resent after a dropped connection.",
	"По сети: скорость ещё не измерена, принято %s/с → ~%s за %d с. Нажмите «Измерить скорость», чтобы уточнить.":              "By network: speed not measured yet, assuming %s/s → ~%s in %d s. Click “Measure speed” to refine.",
	"Прошлый архив был %s — кусок увеличен до %s, чтобы частей было не больше %d.":                                             "The previous archive was %s — the part grows to %s to keep at most %d parts.",
	"Облако принимает файлы не больше %s — кусок уменьшен до %s.":                                                              "The cloud accepts files up to %s — the part is reduced to %s.",
	"Свободной памяти (%s) почти не хватает даже на минимальные куски. Включите буфер на диске или освободите RAM.":            "Free memory (%s) is barely enough even for the smallest parts. Switch to the disk buffer or free some RAM.",
	"Один кусок будет загружаться ~%s. При обрыве он отправляется заново целиком — лучше уменьшить размер.":                    "One part will take ~%s to upload. After a dropped connection it is sent again whole — better make it smaller.",
	"Получится ~%d частей — облако будет медленно показывать папку. Увеличьте кусок, если позволяет память.":                   "That makes ~%d parts — the cloud will list the folder slowly. Increase the part size if memory allows.",
}
