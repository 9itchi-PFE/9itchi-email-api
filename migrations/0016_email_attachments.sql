-- +goose Up
-- The files an application carries besides the CV: the cover letter as a PDF, rendered by resume-api when the email
-- leaves. letter_filename is the name the student gave it, without extension (NULL = the default name);
-- attach_letter is false for the rows made before (they keep leaving as they were); letter_language is the language
-- of the letter (NULL = read from the body).
ALTER TABLE emails
  ADD COLUMN letter_filename text CHECK (char_length(letter_filename) <= 120),
  ADD COLUMN attach_letter boolean NOT NULL DEFAULT false,
  ADD COLUMN letter_language text CHECK (letter_language IN ('fr', 'en', 'ar'));

-- +goose Down
ALTER TABLE emails DROP COLUMN letter_filename, DROP COLUMN attach_letter, DROP COLUMN letter_language;
