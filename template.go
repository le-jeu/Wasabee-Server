package wasabee

import (
	"bytes"
	"cloud.google.com/go/storage"
	"html/template"
	"io/ioutil"
	"path"
	"path/filepath"
)

// XXX this is a kludge
var ts map[string]*template.Template
var funcMap = template.FuncMap{
	"TGGetBotName": TGGetBotName,
	"TGGetBotID":   TGGetBotID,
	"Webroot":      GetWebroot,
	"WebAPIPath":   GetWebAPIPath,
}

// TemplateConfig should be called once from main to establish the templates.
// Individual subsystems should provide their own execution function since requirements will vary
// XXX TODO: establish a way of refreshing/reloading that doesn't leak
//
func TemplateConfig(frontendPath string) (map[string]*template.Template, error) {
	// Transform frontendPath to an absolute path
	fp, err := filepath.Abs(frontendPath)
	if err != nil {
		Log.Fatalw("startup", "error", "frontend path could not be resolved.")
		// panic(err)
	}

	templateSet := make(map[string]*template.Template)

	Log.Debugw("startup", "frontend template directory", fp)
	files, err := ioutil.ReadDir(fp)
	if err != nil {
		Log.Error(err)
		return nil, err
	}

	for _, f := range files {
		lang := f.Name()
		if f.IsDir() && len(lang) == 2 {
			templateSet[lang] = template.New("").Funcs(funcMap) // one funcMap for all languages
			// load the masters
			masterpath := path.Join(fp, "master", "*")
			_, err = templateSet[lang].ParseGlob(masterpath)
			if err != nil {
				Log.Error(err)
			}
			// overwrite with language specific
			langpath := path.Join(fp, lang, "*")
			_, err = templateSet[lang].ParseGlob(langpath)
			if err != nil {
				Log.Error(err)
			}
			Log.Debugw("startup", "language", lang, "templates", templateSet[lang].DefinedTemplates())
		}
	}
	ts = templateSet
	return templateSet, nil
}

// TemplateConfigAppengine is the same as TemplateConfig, but uses a Google Cloud Storage bucket instead of the local filesystem
func TemplateConfigAppengine(bucket *storage.BucketHandle, path string) (map[string]*template.Template, error) {
	templateSet := make(map[string]*template.Template)

	Log.Debugw("frontend templates--function incomplete", "path", path)

	// XXX NOT DONE YET

	ts = templateSet
	return templateSet, nil
}

// ExecuteTemplate formats a message for the user. TBD: language preference.
// Wherever possible, use the message subsystem's templates rather than this (?)
func (gid GoogleID) ExecuteTemplate(name string, data interface{}) (string, error) {
	// XXX lookup agent's language setting
	lang := "en"

	var tpBuffer bytes.Buffer
	if err := ts[lang].ExecuteTemplate(&tpBuffer, name, data); err != nil {
		Log.Info(err)
		return "", err
	}
	return tpBuffer.String(), nil
}
