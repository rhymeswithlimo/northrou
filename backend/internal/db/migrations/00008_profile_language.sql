-- +goose Up
-- Per-profile language preferences (Netflix-style: each viewer picks their own
-- audio/subtitle language). NULL means "fall back to the server default"
-- ([media].preferred_audio_langs / preferred_subtitle_langs).
ALTER TABLE profiles ADD COLUMN preferred_audio_lang TEXT;
ALTER TABLE profiles ADD COLUMN preferred_subtitle_lang TEXT;

-- +goose Down
ALTER TABLE profiles DROP COLUMN preferred_audio_lang;
ALTER TABLE profiles DROP COLUMN preferred_subtitle_lang;
