package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/spf13/cobra"
)

// multipartFieldInfo is the CLI-facing subset of a multipart schema property.
// A binary field consumes a file path; other fields are serialized as form
// values according to their JSON type.
type multipartFieldInfo struct {
	Name        string
	FlagName    string
	Description string
	Type        string
	Required    bool
	Binary      bool
	ContentType string
	Explode     bool
}

func multipartFields(schemaProxy *base.SchemaProxy, encodings *orderedmap.Map[string, *v3.Encoding]) []multipartFieldInfo {
	fields := make([]multipartFieldInfo, 0)
	indexes := map[string]int{}
	required := map[string]bool{}
	seen := map[*base.Schema]bool{}

	var collect func(*base.SchemaProxy)
	collect = func(proxy *base.SchemaProxy) {
		if proxy == nil || proxy.Schema() == nil {
			return
		}
		schema := proxy.Schema()
		if seen[schema] {
			return
		}
		seen[schema] = true

		for _, name := range schema.Required {
			required[name] = true
		}
		for _, parent := range schema.AllOf {
			collect(parent)
		}
		if schema.Properties == nil {
			return
		}
		for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
			name := pair.Key()
			property := pair.Value()
			propertySchema := property.Schema()
			field := multipartFieldInfo{Name: name, FlagName: cliFlagName(name), Type: "string", Explode: true}
			if propertySchema != nil {
				field.Description = propertySchema.Description
				field.Type = schemaType(property)
				field.Binary = strings.EqualFold(propertySchema.Format, "binary") || strings.EqualFold(propertySchema.ContentEncoding, "binary")
				field.ContentType = propertySchema.ContentMediaType
			}
			if encodings != nil {
				if encoding, ok := encodings.Get(name); ok && encoding != nil {
					if encoding.ContentType != "" {
						field.ContentType = encoding.ContentType
					}
					if encoding.Explode != nil {
						field.Explode = *encoding.Explode
					}
					if encoding.ContentType == "application/octet-stream" {
						field.Binary = true
					}
				}
			}

			if index, ok := indexes[name]; ok {
				fields[index] = field
			} else {
				indexes[name] = len(fields)
				fields = append(fields, field)
			}
		}
	}

	collect(schemaProxy)
	for i := range fields {
		fields[i].Required = required[fields[i].Name]
	}
	return fields
}

func registerMultipartFlags(cmd *cobra.Command, fields []multipartFieldInfo) {
	reserved := map[string]bool{
		"body": true, "json-body": true, "schema": true, "field": true, "depth": true,
		"profile": true, "token": true, "base-url": true, "compact": true, "format": true, "help": true,
	}
	for i := range fields {
		field := &fields[i]
		flagName := field.FlagName
		if reserved[flagName] || cmd.Flags().Lookup(flagName) != nil {
			flagName = "form-" + flagName
		}
		field.FlagName = flagName

		description := field.Description
		if description == "" {
			description = "multipart field " + field.Name
		}
		if field.Binary {
			description += " (file path)"
		}
		if field.Required {
			description += " [required unless supplied via --body]"
		}
		cmd.Flags().String(flagName, "", description)
	}
}

func buildMultipartBody(cmd *cobra.Command, rawBody []byte, bodyProvided bool, fields []multipartFieldInfo) ([]byte, string, error) {
	values := map[string]interface{}{}
	if bodyProvided {
		decoder := json.NewDecoder(bytes.NewReader(rawBody))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return nil, "", fmt.Errorf("invalid multipart --body JSON: %w", err)
		}
	}

	for _, field := range fields {
		if !cmd.Flags().Changed(field.FlagName) {
			continue
		}
		value, err := cmd.Flags().GetString(field.FlagName)
		if err != nil {
			return nil, "", fmt.Errorf("reading --%s: %w", field.FlagName, err)
		}
		parsed, err := parseMultipartFlagValue(field, value)
		if err != nil {
			return nil, "", fmt.Errorf("invalid --%s: %w", field.FlagName, err)
		}
		values[field.Name] = parsed
	}

	// Flag-based invocation gets local required-field validation. Raw --body is
	// deliberately passed through more loosely, matching JSON request behavior.
	if !bodyProvided {
		for _, field := range fields {
			if field.Required {
				if _, ok := values[field.Name]; !ok {
					return nil, "", fmt.Errorf("required multipart field %q is missing (use --%s or --body)", field.Name, field.FlagName)
				}
			}
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	known := map[string]bool{}
	for _, field := range fields {
		known[field.Name] = true
		value, ok := values[field.Name]
		if !ok {
			continue
		}
		if err := writeMultipartValue(writer, field, value); err != nil {
			return nil, "", err
		}
	}

	// Preserve extra properties supplied through --body even when the schema
	// permits them without naming them explicitly.
	var extras []string
	for name := range values {
		if !known[name] {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	for _, name := range extras {
		field := multipartFieldInfo{Name: name, Type: inferMultipartType(values[name]), Explode: true}
		if err := writeMultipartValue(writer, field, values[name]); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("finalizing multipart body: %w", err)
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func parseMultipartFlagValue(field multipartFieldInfo, value string) (interface{}, error) {
	if field.Binary || field.Type == "string" || field.Type == "" {
		return value, nil
	}
	switch field.Type {
	case "boolean":
		return strconv.ParseBool(value)
	case "integer":
		return strconv.ParseInt(value, 10, 64)
	case "number":
		return strconv.ParseFloat(value, 64)
	case "array", "object":
		var parsed interface{}
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return nil, fmt.Errorf("expected JSON %s: %w", field.Type, err)
		}
		return parsed, nil
	default:
		return value, nil
	}
}

func writeMultipartValue(writer *multipart.Writer, field multipartFieldInfo, value interface{}) error {
	if values, ok := value.([]interface{}); ok && field.Explode {
		for _, item := range values {
			if err := writeMultipartSingleValue(writer, field, item); err != nil {
				return err
			}
		}
		return nil
	}
	return writeMultipartSingleValue(writer, field, value)
}

func writeMultipartSingleValue(writer *multipart.Writer, field multipartFieldInfo, value interface{}) error {
	if field.Binary {
		path, ok := value.(string)
		if !ok {
			return fmt.Errorf("multipart file field %q must be a file path", field.Name)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening multipart file %q for field %q: %w", path, field.Name, err)
		}
		defer file.Close()

		contentType := field.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name": field.Name, "filename": filepath.Base(path),
		}))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return fmt.Errorf("creating multipart file field %q: %w", field.Name, err)
		}
		if _, err := io.Copy(part, file); err != nil {
			return fmt.Errorf("reading multipart file %q for field %q: %w", path, field.Name, err)
		}
		return nil
	}

	contentType := field.ContentType
	var encoded string
	switch typed := value.(type) {
	case string:
		encoded = typed
	case nil:
		encoded = "null"
	case json.Number:
		encoded = typed.String()
	case bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		encoded = fmt.Sprint(typed)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encoding multipart field %q: %w", field.Name, err)
		}
		encoded = string(data)
		if contentType == "" {
			contentType = "application/json"
		}
	}

	if contentType == "" {
		part, err := writer.CreateFormField(field.Name)
		if err != nil {
			return fmt.Errorf("creating multipart field %q: %w", field.Name, err)
		}
		_, err = io.WriteString(part, encoded)
		return err
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": field.Name}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("creating multipart field %q: %w", field.Name, err)
	}
	_, err = io.WriteString(part, encoded)
	return err
}

func inferMultipartType(value interface{}) string {
	switch value.(type) {
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	case bool:
		return "boolean"
	case json.Number, float64:
		return "number"
	default:
		return "string"
	}
}
