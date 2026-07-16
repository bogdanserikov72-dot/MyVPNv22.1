# Libraries

Place the generated Gomobile AAR file here.
The expected file name is `gomobile-intouristcore.aar` (or adjust the `app/build.gradle` to match).

To generate it, run:
gomobile bind -target=android -androidapi=23 -javapkg=com.intourist.gomobile ./mobile/intouristcore
