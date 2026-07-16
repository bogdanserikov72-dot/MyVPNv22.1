# Инструкция по сборке Android-версии Intourist VPN

## Выполненные изменения

### 1. Создан файл mobile/intouristcore/configgen.go
Создана функция HelperConfigFromServerData() для конвертации JSON → YAML.

### 2. Обновлен mobile/intouristcore/intouristcore.go
Добавлены импорты для yaml и config. StartHelperMode() теперь автоматически конвертирует JSON в YAML.

## Проблема
gomobile не может найти компилятор C из-за пробелов в пути (C:\Users\Halo Wolf)

## Решение
Установить компилятор C (MSVC или Clang) и собрать AAR:
cd D:\MyVPNv22.1\mobile
gomobile bind -target=android -androidapi=21 -javapkg=\"com.intourist.gomobile\" ./intouristcore

## Измененные файлы
- mobile/intouristcore/configgen.go (новый)
- mobile/intouristcore/intouristcore.go (обновлён)
