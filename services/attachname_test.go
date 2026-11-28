package services

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanStem(t *testing.T) {
	for in, want := range map[string]string{
		"Lettre de motivation":            "Lettre de motivation",
		"  ../../etc/Ma lettre.PDF ":      "Ma lettre",
		`C:\Users\ines\cv.docx`:           "cv",
		"lettre.doc":                      "lettre",
		"a<b>c:d|e?f*g\"h":                "a_b_c_d_e_f_g_h",
		"Ines   __  Ben__Salah":           "Ines Ben_Salah",
		"CV Inès — Data":                  "CV Inès Data",
		"Ines     Ben":                    "Ines Ben",
		"-._ (Stage) PFE 2026 ._-":        "(Stage) PFE 2026",
		"رسالة تحفيزية":                   "رسالة تحفيزية",
		"Développeuse":                    "Développeuse",
		"line\r\nbreak":                   "line_break",
		"   ":                             "fallback",
		"/.pdf":                           "fallback",
		strings.Repeat("é", 100) + ".pdf": strings.Repeat("é", 80),
	} {
		assert.Equal(t, want, cleanStem(in, "fallback"), in)
	}
}

func TestDefaultLetterStemAndLanguage(t *testing.T) {
	assert.Equal(t, "Lettre_de_motivation_Ines_Ben_Salah", defaultLetterStem("fr", " Ines  Ben Salah "))
	assert.Equal(t, "Cover_letter_Élodie_Dupont", defaultLetterStem("en", "Élodie Dupont"))
	assert.Equal(t, "رسالة_تحفيزية_إيناس_بن_صالح", defaultLetterStem("ar", "إيناس بن صالح"))
	assert.Equal(t, "Lettre_de_motivation", defaultLetterStem("fr", ""))
	assert.Equal(t, "Lettre_de_motivation", defaultLetterStem("de", ""))

	assert.Equal(t, "Développeuse React", cvStem("Développeuse React", "Ines"))
	assert.Equal(t, "Ines_Ben", cvStem(" ", "Ines Ben"))
	assert.Equal(t, "CV", cvStem("", ""))

	assert.Equal(t, "en", letterLanguage("en", "السلام عليكم"))
	assert.Equal(t, "ar", letterLanguage("", "السلام عليكم، أتقدم بطلب تربص PFE"))
	assert.Equal(t, "fr", letterLanguage("", "Bonjour, je joins mon CV (السيرة)"))
	assert.Equal(t, "fr", letterLanguage("", ""))
}

// recordLetters is a harness whose resume-api records the letters it renders.
func recordLetters(resumes fakeResumes) (*harness, *[]core.LetterRequest) {
	letters := &[]core.LetterRequest{}
	resumes.letters = letters
	return newHarnessWith(nil, unlimited, resumes), letters
}

func TestSendCarriesTheCVAndTheLetterByDefault(t *testing.T) {
	h, letters := recordLetters(fakeResumes{pdf: []byte("%PDF cv"), pdfName: "Développeuse React.pdf"})
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	assert.True(t, e.AttachLetter)
	assert.Nil(t, e.LetterFilename)
	require.NotNil(t, e.LetterLanguage)
	assert.Equal(t, "fr", *e.LetterLanguage)

	require.Len(t, h.mailer.sent, 1)
	att := h.mailer.sent[0].Attachments
	require.Len(t, att, 2)
	assert.Equal(t, "Développeuse React.pdf", att[0].Filename, "the name resume-api gives the CV")
	assert.Equal(t, []byte("%PDF cv"), att[0].Data)
	assert.Equal(t, "Lettre_de_motivation_Ines_Ben_Salah.pdf", att[1].Filename)
	assert.Equal(t, "application/pdf", att[1].ContentType)
	assert.Equal(t, []byte("%PDF letter"), att[1].Data)

	require.Len(t, *letters, 1)
	assert.Equal(t, core.LetterRequest{ResumeID: resumID, Language: "fr", Subject: "Candidature", Body: "Salut <b>\nBonjour", SenderEmail: "me@gmail.com"}, (*letters)[0])
}

func TestSendWithACustomLetterName(t *testing.T) {
	h := newHarness(nil, unlimited)
	req := sendReq()
	req.LetterFilename = " ../Ma lettre.PDF "
	e, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	require.NotNil(t, e.LetterFilename)
	assert.Equal(t, "Ma lettre", *e.LetterFilename)
	assert.Equal(t, "Ma lettre.pdf", h.mailer.sent[0].Attachments[1].Filename)
}

func TestSendWithoutTheLetter(t *testing.T) {
	h, letters := recordLetters(fakeResumes{pdf: []byte("%PDF")})
	req := sendReq()
	req.AttachLetter = ptr(false)
	e, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.False(t, e.AttachLetter)
	require.Len(t, h.mailer.sent[0].Attachments, 1)
	assert.Equal(t, "CV.pdf", h.mailer.sent[0].Attachments[0].Filename)
	assert.Empty(t, *letters)
}

func TestSendWithoutCVCarriesTheLetterOnly(t *testing.T) {
	h, letters := recordLetters(fakeResumes{pdf: []byte("%PDF")})
	req := sendReq()
	req.ResumeID = ""
	_, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	require.Len(t, h.mailer.sent[0].Attachments, 1)
	assert.Equal(t, "Lettre_de_motivation.pdf", h.mailer.sent[0].Attachments[0].Filename)
	require.Len(t, *letters, 1)
	assert.Empty(t, (*letters)[0].ResumeID, "no CV: no contact block")
}

func TestImportedFileKeepsItsNameAndExtension(t *testing.T) {
	h := newHarnessWith(nil, noPlan, fakeResumes{original: &core.Attachment{
		Filename: "Développeuse React.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: []byte("PK"),
	}})
	_, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.Nil(t, apiErr)
	att := h.mailer.sent[0].Attachments
	require.Len(t, att, 2)
	assert.Equal(t, "Développeuse React.docx", att[0].Filename)
	assert.Equal(t, "Lettre_de_motivation_Ines_Ben_Salah.pdf", att[1].Filename)
}

func TestSendFailsWhenTheLetterCannotBeAttached(t *testing.T) {
	h := newHarnessWith(nil, unlimited, fakeResumes{pdf: []byte("%PDF"), letterErr: core.ErrPeerUnavailable})
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)
	assert.Equal(t, "the cover letter could not be attached", apiErr.Error.Error())
	assert.Equal(t, db.EmailFailed, e.Status)
	assert.Equal(t, db.EmailFailed, h.repo.rows[e.ID].Status)
	assert.Empty(t, h.mailer.sent)
}

func TestLetterNamedLikeTheCVIsRefused(t *testing.T) {
	h := newHarness(nil, unlimited)
	req := sendReq()
	req.LetterFilename = "développeuse react.pdf"
	_, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)
	assert.Equal(t, "letter_filename must differ from the CV name", apiErr.Error.Error())
	assert.Empty(t, h.repo.rows)

	// No letter attached: its name does not matter.
	req.AttachLetter = ptr(false)
	_, apiErr = h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
}

func TestArabicBodyGivesAnArabicLetter(t *testing.T) {
	h, letters := recordLetters(fakeResumes{pdf: []byte("%PDF")})
	req := sendReq()
	req.Body = "السلام عليكم، أتقدم بطلب تربص ختم الدروس."
	e, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "ar", *e.LetterLanguage)
	assert.Equal(t, "ar", (*letters)[0].Language)
	assert.Equal(t, "رسالة_تحفيزية_Ines_Ben_Salah.pdf", h.mailer.sent[0].Attachments[1].Filename)
}

func TestLetterNameWhenTheCVNameCannotBeRead(t *testing.T) {
	h := newHarnessWith(nil, unlimited, fakeResumes{pdf: []byte("%PDF"), namesErr: core.ErrPeerUnavailable})
	req := sendReq()
	req.Language = "en"
	_, apiErr := h.svc.SendEmail(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "Cover_letter.pdf", h.mailer.sent[0].Attachments[1].Filename)
}

func TestSendTooLargeForGmail(t *testing.T) {
	h := newHarness(nil, unlimited)
	h.mailer.err = core.ErrMailTooLarge
	e, apiErr := h.svc.SendEmail(context.Background(), uid, sendReq())
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code)
	assert.Equal(t, "the attachments are larger than Gmail allows (25 MB)", apiErr.Error.Error())
	assert.Equal(t, db.EmailFailed, e.Status)
}

func TestUpdateEmailLetter(t *testing.T) {
	h := newHarness(nil, unlimited)
	req := sendReq()
	req.LetterFilename = "Ma lettre"
	e := scheduled(t, h, req)
	require.Equal(t, "Ma lettre", *e.LetterFilename)

	out, apiErr := h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{LetterFilename: ptr("Lettre Acme"), Language: ptr("en")})
	require.Nil(t, apiErr)
	assert.Equal(t, "Lettre Acme", *out.LetterFilename)
	assert.Equal(t, "en", *out.LetterLanguage)
	assert.True(t, out.AttachLetter)

	_, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{LetterFilename: ptr("Développeuse React")})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)
	assert.Equal(t, "Lettre Acme", *h.repo.rows[e.ID].LetterFilename, "nothing changed")

	out, apiErr = h.svc.UpdateEmail(context.Background(), uid, e.ID, api.UpdateEmailRequest{LetterFilename: ptr(""), AttachLetter: ptr(false)})
	require.Nil(t, apiErr)
	assert.Nil(t, out.LetterFilename, "back to the default name")
	assert.False(t, out.AttachLetter)
	assert.Equal(t, "en", *out.LetterLanguage, "unchanged")
}

func TestDispatchUsesTheStoredLetter(t *testing.T) {
	h, letters := recordLetters(fakeResumes{pdf: []byte("%PDF")})
	req := sendReq()
	req.LetterFilename, req.Language = "Cover letter Acme", "en"
	e := scheduled(t, h, req)
	out, apiErr := dispatchAt(h.svc, tomorrow, e.ID)
	require.Nil(t, apiErr)
	assert.Equal(t, db.EmailSent, out.Status)
	require.Len(t, h.mailer.sent, 1)
	assert.Equal(t, "Cover letter Acme.pdf", h.mailer.sent[0].Attachments[1].Filename)
	assert.Equal(t, "en", (*letters)[0].Language)

	// A row made before the letter existed leaves as it was: without a letter.
	h, letters = recordLetters(fakeResumes{pdf: []byte("%PDF")})
	old, err := h.repo.Create(context.Background(), &db.Email{UserID: uid, ResumeID: ptr(resumID), Recipient: "hr@acme.tn", Subject: "s", Body: "b", Status: db.EmailScheduled, ScheduledAt: &tomorrow})
	require.NoError(t, err)
	_, apiErr = dispatchAt(h.svc, tomorrow, old.ID)
	require.Nil(t, apiErr)
	assert.Len(t, h.mailer.sent[0].Attachments, 1)
	assert.Empty(t, *letters)
}

func TestAttachmentNames(t *testing.T) {
	h := newHarness(nil, unlimited)
	ctx := context.Background()
	out, apiErr := h.svc.AttachmentNames(ctx, uid, api.AttachmentNamesQuery{ResumeID: resumID})
	require.Nil(t, apiErr)
	assert.Equal(t, api.AttachmentNamesResponse{CVFilename: "Développeuse React.pdf", LetterFilename: "Lettre_de_motivation_Ines_Ben_Salah"}, *out)

	out, apiErr = h.svc.AttachmentNames(ctx, uid, api.AttachmentNamesQuery{Language: "ar"})
	require.Nil(t, apiErr)
	assert.Equal(t, api.AttachmentNamesResponse{CVFilename: "", LetterFilename: "رسالة_تحفيزية"}, *out)

	h = newHarnessWith(nil, unlimited, fakeResumes{namesErr: core.ErrPeerNotFound})
	_, apiErr = h.svc.AttachmentNames(ctx, uid, api.AttachmentNamesQuery{ResumeID: resumID})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)

	h = newHarnessWith(nil, unlimited, fakeResumes{namesErr: core.ErrPeerUnavailable})
	out, apiErr = h.svc.AttachmentNames(ctx, uid, api.AttachmentNamesQuery{ResumeID: resumID, Language: "en"})
	require.Nil(t, apiErr)
	assert.Equal(t, api.AttachmentNamesResponse{CVFilename: "CV.pdf", LetterFilename: "Cover_letter"}, *out)
}
