# Если UI файл не найден («UI файл не найден» в окне)

## Быстрое решение

1. **Проверь структуру после сборки:**
   ```
   dist/IntouristVPN_GUI/
   ├── intourist_vpn_gui.exe
   ├── intourist_vps_premium_ui/          ← папка должна быть ЗДЕСЬ
   │   ├── index.html
   │   ├── style.css
   │   └── images/
   ├── _internal/
   ├── myvpn.exe
   ├── wintun.dll
   └── ...
   ```

2. **Если папки нет — скопируй вручную:**
   ```powershell
   # Из корня проекта:
   Copy-Item -Path "intourist_vps_premium_ui" -Destination "dist\IntouristVPN_GUI\" -Recurse -Force
   ```

3. **Перезапусти exe.**

---

## Если это не поможет

### Вариант 1: Проверка через источник (разработка)
```bash
python myvpn_gui.py
```
Если из исходников запускается — проблема в сборке PyInstaller.

### Вариант 2: Пересобрать с явным указанием
```powershell
# Очистить старую сборку
rm -r dist
rm -r build
rm *.spec

# Собрать заново
pyinstaller IntouristVPN_GUI.spec --distpath dist --buildpath build
```

### Вариант 3: Проверить, куда PyInstaller положил файлы
```powershell
# Откройте dist\IntouristVPN_GUI\ в проводнике
# Ищите intourist_vps_premium_ui/
#  - может быть в _internal/ или в самой папке
# Если нашли в _internal/ — отредактируйте myvpn_gui.py,
#   раскомментируйте строку для поиска в _internal/
```

---

## Почему это происходит

- `intourist_vps_premium_ui/` — папка с веб-интерфейсом (HTML/CSS/иконки)
- PyInstaller при сборке должен скопировать её рядом с exe
- Если папка не скопировалась → UI не загружается

Новая версия `myvpn_gui.py` пытается найти папку в нескольких местах и показывает диагностику.

---

## Для разработки (без сборки)

Просто запусти `python myvpn_gui.py` из корня проекта — всё должно найтись.
