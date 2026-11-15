package googletype

import "google.golang.org/protobuf/reflect/protoreflect"

// fields documents every field of every google.type message. The examples
// are realistic and valid for googleapis' comments, which the descriptions
// shorten. The examples of one message describe one value, a place or a
// moment, except where that value leaves a field unused: a US address has
// no sorting code, so PostalAddress.sorting_code shows a French one. Enums
// need none: an enum's example is one of its values.
var fields = map[protoreflect.FullName]entry{
	// google.type.Color
	"google.type.Color.red":   {`0.26`, "The amount of red in the color as a value in the interval [0, 1]."},
	"google.type.Color.green": {`0.52`, "The amount of green in the color as a value in the interval [0, 1]."},
	"google.type.Color.blue":  {`0.96`, "The amount of blue in the color as a value in the interval [0, 1]."},
	"google.type.Color.alpha": {`1`, "The fraction of this color that should be applied to the pixel: 1.0 is a solid color, 0.0 a completely transparent one. If omitted, the color is rendered as a solid color."},

	// google.type.Date
	"google.type.Date.year":  {`2026`, "Year of the date. Must be from 1 to 9999, or 0 to specify a date without a year."},
	"google.type.Date.month": {`11`, "Month of a year. Must be from 1 to 12, or 0 to specify a year without a month and day."},
	"google.type.Date.day":   {`14`, "Day of a month. Must be from 1 to 31 and valid for the year and month, or 0 to specify a year by itself or a year and month where the day isn't significant."},

	// google.type.DateTime and google.type.TimeZone
	"google.type.DateTime.year":       {`2026`, "Year of date. Must be from 1 to 9999, or 0 if specifying a datetime without a year."},
	"google.type.DateTime.month":      {`11`, "Month of year. Must be from 1 to 12, or 0 if specifying a datetime without a month."},
	"google.type.DateTime.day":        {`14`, "Day of month. Must be from 1 to 31 and valid for the year and month, or 0 if specifying a datetime without a day."},
	"google.type.DateTime.hours":      {`9`, "Hours of day in 24 hour format. Should be from 0 to 23, defaults to 0 (midnight)."},
	"google.type.DateTime.minutes":    {`30`, "Minutes of hour of day. Must be from 0 to 59, defaults to 0."},
	"google.type.DateTime.seconds":    {`0`, "Seconds of minutes of the time. Must normally be from 0 to 59, defaults to 0."},
	"google.type.DateTime.nanos":      {`0`, "Fractions of seconds in nanoseconds. Must be from 0 to 999,999,999, defaults to 0."},
	"google.type.DateTime.utc_offset": {`"-18000s"`, "UTC offset. Must be whole seconds, between -18 hours and +18 hours. Set either it or the time zone; with neither, the DateTime is in local time."},
	"google.type.DateTime.time_zone":  {``, "Time zone. Set either it or the UTC offset; with neither, the DateTime is in local time."},
	"google.type.TimeZone.id":         {`"America/New_York"`, "IANA Time Zone Database time zone, such as \"America/New_York\"."},
	"google.type.TimeZone.version":    {`"2019a"`, "IANA Time Zone Database version number, such as \"2019a\"."},

	// google.type.Decimal
	"google.type.Decimal.value": {`"12.50"`, "The decimal value, as a string: an optional sign, digits, an optional fraction after a \".\" and an optional exponent, such as \"2.5\" or \"-2.5E+8\". An empty string is 0."},

	// google.type.Expr
	"google.type.Expr.expression":  {`"document.summary.size() < 100"`, "Textual representation of an expression in Common Expression Language syntax."},
	"google.type.Expr.title":       {`"Summary size limit"`, "Title for the expression: a short string describing its purpose."},
	"google.type.Expr.description": {`"Determines if a summary is less than 100 chars"`, "Description of the expression: a longer text which describes it."},
	"google.type.Expr.location":    {`"policies/documents.yaml:12"`, "The location of the expression for error reporting, such as a file name and a position in the file."},

	// google.type.Fraction
	"google.type.Fraction.numerator":   {`"2"`, "The numerator in the fraction, such as 2 in 2/3."},
	"google.type.Fraction.denominator": {`"3"`, "The value by which the numerator is divided, such as 3 in 2/3. Must be positive."},

	// google.type.Interval
	"google.type.Interval.start_time": {`"2026-11-14T09:00:00Z"`, "Inclusive start of the interval. If specified, a Timestamp matching this interval will have to be the same or after the start."},
	"google.type.Interval.end_time":   {`"2026-11-14T17:00:00Z"`, "Exclusive end of the interval. If specified, a Timestamp matching this interval will have to be before the end."},

	// google.type.LatLng
	"google.type.LatLng.latitude":  {`37.422`, "The latitude in degrees. It must be in the range [-90.0, +90.0]."},
	"google.type.LatLng.longitude": {`-122.084`, "The longitude in degrees. It must be in the range [-180.0, +180.0]."},

	// google.type.LocalizedText
	"google.type.LocalizedText.text":          {`"Hello, world."`, "Localized string in the language of the language code."},
	"google.type.LocalizedText.language_code": {`"en-US"`, "The text's BCP-47 language code, such as \"en-US\" or \"sr-Latn\"."},

	// google.type.Money
	"google.type.Money.currency_code": {`"USD"`, "The three-letter currency code defined in ISO 4217."},
	"google.type.Money.units":         {`"42"`, "The whole units of the amount. For example if currencyCode is \"USD\", then 1 unit is one US dollar."},
	"google.type.Money.nanos":         {`500000000`, "Number of nano (10^-9) units of the amount, from -999,999,999 to +999,999,999, with the sign of units when units is not zero. For example $-1.75 is units -1 and nanos -750,000,000."},

	// google.type.PhoneNumber
	"google.type.PhoneNumber.e164_number":           {`"+15552220123"`, "The phone number: a leading plus sign followed by the country calling code and the subscriber number, with no spaces or formatting, in ITU E.164 format, such as \"+15552220123\". Set either it or a short code."},
	"google.type.PhoneNumber.short_code":            {``, "A short code. Set either it or an E.164 number."},
	"google.type.PhoneNumber.extension":             {`"123"`, "The phone number's extension: a series of up to 40 digits, and dialing characters such as \",\" (a wait) or \"#\"."},
	"google.type.PhoneNumber.ShortCode.region_code": {`"US"`, "The BCP-47 region code of the location where calls to this short code can be made, such as \"US\" and \"BB\"."},
	"google.type.PhoneNumber.ShortCode.number":      {`"611"`, "The short code digits, without a leading plus ('+') or country calling code, such as \"611\"."},

	// google.type.PostalAddress
	"google.type.PostalAddress.revision":            {`0`, "The schema revision of the PostalAddress. This must be set to 0, which is the latest revision."},
	"google.type.PostalAddress.region_code":         {`"US"`, "CLDR region code of the country or region of the address, such as \"CH\" for Switzerland. Required; it is never inferred."},
	"google.type.PostalAddress.language_code":       {`"en"`, "BCP-47 language code of the contents of this address, if known, such as \"zh-Hant\", \"ja\", \"ja-Latn\" or \"en\"."},
	"google.type.PostalAddress.postal_code":         {`"94043"`, "Postal code of the address. Not all countries use or require postal codes."},
	"google.type.PostalAddress.sorting_code":        {`"CEDEX 7"`, "Additional, country-specific, sorting code, such as \"CEDEX 7\" in France. This is not used in most regions."},
	"google.type.PostalAddress.administrative_area": {`"CA"`, "Highest administrative subdivision which is used for postal addresses of a country or region: a state, a province, an oblast, or a prefecture."},
	"google.type.PostalAddress.locality":            {`"Mountain View"`, "Generally refers to the city or town portion of the address."},
	"google.type.PostalAddress.sublocality":         {`"North Bayshore"`, "Sublocality of the address, such as a neighborhood, borough, or district."},
	"google.type.PostalAddress.address_lines":       {`["1600 Amphitheatre Parkway"]`, "Unstructured address lines describing the lower levels of an address, in envelope order for the country or region of the address."},
	"google.type.PostalAddress.recipients":          {`["Jane Doe"]`, "The recipient at the address. This field may contain multiline information, such as \"care of\" information."},
	"google.type.PostalAddress.organization":        {`"Google LLC"`, "The name of the organization at the address."},

	// google.type.Quaternion: a quarter turn about the z axis.
	"google.type.Quaternion.x": {`0`, "The x component."},
	"google.type.Quaternion.y": {`0`, "The y component."},
	"google.type.Quaternion.z": {`0.7071068`, "The z component."},
	"google.type.Quaternion.w": {`0.7071068`, "The scalar component."},

	// google.type.TimeOfDay
	"google.type.TimeOfDay.hours":   {`9`, "Hours of a day in 24 hour format. Must be greater than or equal to 0 and typically must be less than or equal to 23."},
	"google.type.TimeOfDay.minutes": {`30`, "Minutes of an hour. Must be greater than or equal to 0 and less than or equal to 59."},
	"google.type.TimeOfDay.seconds": {`0`, "Seconds of a minute. Must be greater than or equal to 0 and typically must be less than or equal to 59."},
	"google.type.TimeOfDay.nanos":   {`0`, "Fractions of seconds, in nanoseconds. Must be greater than or equal to 0 and less than or equal to 999,999,999."},
}
