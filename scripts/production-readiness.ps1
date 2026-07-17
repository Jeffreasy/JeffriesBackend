#requires -Version 7.0
<#
Safe production-readiness helper for JeffriesBackend, JeffriesHomeapp,
LaventeCareFrontend and LaventeCareAuthSystems. Check mode is read-only and
never prints environment values.

Exit 0 = all assessed gates passed; exit 1 = known blocker; exit 2 = required
check could not be assessed. UNKNOWN is therefore never silently green.
#>

[CmdletBinding()]
param(
    [ValidateSet("Check", "HomeappAuthE2E", "OpenDashboards")]
    [string]$Mode = "Check",
    [switch]$SkipNetwork,
    [string]$AuthStatePath = (Join-Path ([IO.Path]::GetTempPath()) "jeffries-homeapp-e2e-auth.json"),
    [switch]$KeepAuthState,
    [switch]$OverwriteAuthState,
    [ValidateRange(5, 120)]
    [int]$NetworkTimeoutSec = 25,
    [string]$IntendedHomeappSha
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false
if (-not $IsWindows) {
    Write-Error "Deze helper beheert Windows-processen en mag alleen op Windows worden uitgevoerd."
    exit 1
}

$script:Results = [Collections.Generic.List[object]]::new()
$script:VercelProjectId = "prj_RISZ0opLb1NSVbj8FodNWaEgAm7S"
$script:VercelTeamId = "team_BXrDmTQBlk2ykC0fHjVMxPjn"
$script:VercelScope = "jeffreys-projects-c48521c1"
$script:VercelProjectName = "jeffries-homeapp"

function Add-Result {
    param(
        [ValidateSet("PASS", "UNKNOWN", "FAIL", "INFO")][string]$State,
        [string]$Name,
        [string]$Detail
    )
    $color = switch ($State) {
        "PASS" { "Green" }
        "UNKNOWN" { "Yellow" }
        "FAIL" { "Red" }
        default { "Gray" }
    }
    $script:Results.Add([pscustomobject]@{ State = $State; Name = $Name; Detail = $Detail })
    Write-Host ("[{0,-7}] {1}: {2}" -f $State, $Name, $Detail) -ForegroundColor $color
}

function Get-CommandAvailable {
    param([string]$Name)
    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Read-DotEnv {
    param([string]$Path)
    $values = @{}
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $values }

    $lines = [IO.File]::ReadAllLines($Path)
    for ($index = 0; $index -lt $lines.Length; $index++) {
        $line = $lines[$index]
        if ($line -notmatch '^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') { continue }
        $name = $Matches[1]
        $value = $Matches[2].Trim()
        if ($value.StartsWith('"') -or $value.StartsWith("'")) {
            $quote = $value[0]
            $parts = [Collections.Generic.List[string]]::new()
            $parts.Add($value)
            while (-not $parts[-1].TrimEnd().EndsWith([string]$quote)) {
                $index++
                if ($index -ge $lines.Length) { break }
                $parts.Add($lines[$index])
            }
            $value = ($parts -join "`n").TrimEnd()
            if ($value.Length -ge 2 -and $value[0] -eq $quote -and $value[-1] -eq $quote) {
                $value = $value.Substring(1, $value.Length - 2)
            }
        }
        else {
            $value = [regex]::Replace($value, '\s+#.*$', '').Trim()
        }
        $values[$name] = $value
    }
    return $values
}

function Get-EnvironmentSnapshot {
    param([string[]]$Paths)
    $values = @{}
    $sources = [Collections.Generic.List[string]]::new()
    foreach ($path in $Paths) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        $sources.Add($path)
        $layer = Read-DotEnv -Path $path
        foreach ($name in $layer.Keys) { $values[$name] = $layer[$name] }
    }
    return [pscustomobject]@{ Values = $values; Sources = @($sources) }
}

function Test-IsPlaceholder {
    param([AllowNull()][string]$Value)
    if ([string]::IsNullOrWhiteSpace($Value)) { return $true }
    $trimmed = $Value.Trim()
    if ($trimmed -match '^\$\{[^}]+\}$' -or $trimmed -match '^<[^>]+>$') { return $true }
    if ($trimmed -match '(?i)(change[-_ ]?me|replace[-_ ]?me|your[-_ ]|your\.|placeholder|dummy|xxxxxx|insert[-_ ]?here|example\.com)') { return $true }
    if ($trimmed -match '^(?:0+|1+|a+|x+)$') { return $true }
    return $false
}

function Test-SecretShape {
    param([AllowNull()][string]$Value, [int]$MinimumLength = 32)
    return -not (Test-IsPlaceholder $Value) -and $Value.Trim().Length -ge $MinimumLength
}

function Test-AbsoluteUrl {
    param([AllowNull()][string]$Value, [string[]]$AllowedSchemes = @("https"), [switch]$AllowLoopbackHttp)
    if (Test-IsPlaceholder $Value) { return $false }
    $uri = $null
    if (-not [Uri]::TryCreate($Value.Trim(), [UriKind]::Absolute, [ref]$uri)) { return $false }
    if ([string]::IsNullOrWhiteSpace($uri.Host) -or $AllowedSchemes -notcontains $uri.Scheme.ToLowerInvariant()) { return $false }
    if ($uri.Scheme -eq "http" -and -not ($AllowLoopbackHttp -and $uri.IsLoopback)) { return $false }
    return $true
}

function Test-ServiceUrl {
    param([AllowNull()][string]$Value, [string[]]$Schemes)
    if (Test-IsPlaceholder $Value) { return $false }
    $uri = $null
    if (-not [Uri]::TryCreate($Value.Trim(), [UriKind]::Absolute, [ref]$uri)) { return $false }
    return $uri.Scheme -in $Schemes -and -not [string]::IsNullOrWhiteSpace($uri.Host)
}

function Get-RsaPrivateKeyIssue {
    param([AllowNull()][string]$Value)
    if (Test-IsPlaceholder $Value) { return "ontbreekt of is placeholder" }
    $pem = $Value.Replace('\n', "`n").Trim()
    $match = [regex]::Match($pem, '(?s)^-----BEGIN (RSA )?PRIVATE KEY-----\s*(.*?)\s*-----END (RSA )?PRIVATE KEY-----$')
    if (-not $match.Success -or $match.Groups[1].Value -ne $match.Groups[3].Value) { return "is geen ondersteunde PEM private key" }
    try {
        $der = [Convert]::FromBase64String(($match.Groups[2].Value -replace '\s', ''))
        $rsa = [Security.Cryptography.RSA]::Create()
        try {
            $read = 0
            if ($match.Groups[1].Value -eq "RSA ") { $rsa.ImportRSAPrivateKey($der, [ref]$read) }
            else { $rsa.ImportPkcs8PrivateKey($der, [ref]$read) }
            if ($read -ne $der.Length) { return "bevat onverwachte trailing key-data" }
            if ($rsa.KeySize -lt 2048) { return "is kleiner dan 2048 bit" }
        }
        finally { $rsa.Dispose() }
    }
    catch { return "kan niet als RSA private key worden geïmporteerd" }
    return $null
}

function Add-ContractResult {
    param([string]$Label, [object]$Snapshot, [Collections.Generic.List[string]]$Problems, [int]$AssessedNames)
    if ($Snapshot.Sources.Count -eq 0) { Add-Result "UNKNOWN" "$Label env" "geen lokaal dotenv-bestand; productieconfig is niet beoordeeld" }
    elseif ($Problems.Count -gt 0) { Add-Result "FAIL" "$Label env" ($Problems -join '; ') }
    else { Add-Result "PASS" "$Label env" "$AssessedNames namen en formaten beoordeeld; waarden niet getoond" }
}

function Test-BackendEnvironment {
    param([string]$Root)
    $snapshot = Get-EnvironmentSnapshot @((Join-Path $Root ".env"))
    $env = $snapshot.Values
    $problems = [Collections.Generic.List[string]]::new()
    if (-not $env.ContainsKey("DATABASE_URL") -or -not (Test-ServiceUrl $env["DATABASE_URL"] @("postgres", "postgresql"))) { $problems.Add("DATABASE_URL ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("APP_SECRET_KEY") -or -not (Test-SecretShape $env["APP_SECRET_KEY"])) { $problems.Add("APP_SECRET_KEY ontbreekt/is zwak") }
    if (-not $env.ContainsKey("HOMEAPP_USER_ID") -or [string]$env["HOMEAPP_USER_ID"] -notmatch '^user_[A-Za-z0-9]{12,}$') { $problems.Add("HOMEAPP_USER_ID ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("LAVENTECARE_SECRET_KEY") -or -not (Test-SecretShape $env["LAVENTECARE_SECRET_KEY"])) { $problems.Add("LAVENTECARE_SECRET_KEY ontbreekt/is zwak") }
    if (-not $env.ContainsKey("LAVENTECARE_INTAKE_SECRET") -or -not (Test-SecretShape $env["LAVENTECARE_INTAKE_SECRET"])) { $problems.Add("LAVENTECARE_INTAKE_SECRET ontbreekt/is zwak") }
    $queueMode = $env.ContainsKey("LIGHT_COMMAND_MODE") -and [string]$env["LIGHT_COMMAND_MODE"] -eq "queue"
    $bridgeConfigured = $env.ContainsKey("BRIDGE_API_KEY") -and -not [string]::IsNullOrWhiteSpace([string]$env["BRIDGE_API_KEY"])
    if (($queueMode -or $bridgeConfigured) -and -not (Test-SecretShape $env["BRIDGE_API_KEY"])) { $problems.Add("BRIDGE_API_KEY ontbreekt/is zwak voor bridge/queue-mode") }
    Add-ContractResult "Backend" $snapshot $problems 5
    return $env
}

function Test-HomeappEnvironment {
    param([string]$Root)
    $snapshot = Get-EnvironmentSnapshot @((Join-Path $Root ".env"), (Join-Path $Root ".env.local"))
    $env = $snapshot.Values
    $problems = [Collections.Generic.List[string]]::new()
    if (-not $env.ContainsKey("NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY") -or [string]$env["NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY"] -notmatch '^pk_(?:test|live)_[A-Za-z0-9_-]{12,}$') { $problems.Add("NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("CLERK_SECRET_KEY") -or [string]$env["CLERK_SECRET_KEY"] -notmatch '^sk_(?:test|live)_[A-Za-z0-9_-]{12,}$') { $problems.Add("CLERK_SECRET_KEY ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("BACKEND_API_URL") -or -not (Test-AbsoluteUrl $env["BACKEND_API_URL"] @("http", "https") -AllowLoopbackHttp)) { $problems.Add("BACKEND_API_URL ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("BACKEND_API_KEY") -or -not (Test-SecretShape $env["BACKEND_API_KEY"])) { $problems.Add("BACKEND_API_KEY ontbreekt/is zwak") }
    if (-not $env.ContainsKey("HOMEAPP_OWNER_USER_ID") -or [string]$env["HOMEAPP_OWNER_USER_ID"] -notmatch '^user_[A-Za-z0-9]{12,}$') { $problems.Add("HOMEAPP_OWNER_USER_ID ontbreekt/is ongeldig") }
    Add-ContractResult "Homeapp" $snapshot $problems 5
    return $env
}

function Test-PublicEnvironment {
    param([string]$Root)
    $snapshot = Get-EnvironmentSnapshot @((Join-Path $Root ".env"))
    $env = $snapshot.Values
    $problems = [Collections.Generic.List[string]]::new()
    if (-not $env.ContainsKey("PUBLIC_API_URL") -or -not (Test-AbsoluteUrl $env["PUBLIC_API_URL"] @("http", "https") -AllowLoopbackHttp)) { $problems.Add("PUBLIC_API_URL ontbreekt/is ongeldig") }
    $tenantId = [guid]::Empty
    if (-not $env.ContainsKey("PUBLIC_TENANT_ID") -or -not [guid]::TryParse([string]$env["PUBLIC_TENANT_ID"], [ref]$tenantId) -or $tenantId -eq [guid]::Empty) { $problems.Add("PUBLIC_TENANT_ID ontbreekt/is geen UUID") }
    if ($env.ContainsKey("PUBLIC_JWT_ISSUER") -and -not [string]::IsNullOrWhiteSpace([string]$env["PUBLIC_JWT_ISSUER"]) -and -not (Test-AbsoluteUrl $env["PUBLIC_JWT_ISSUER"] @("http", "https") -AllowLoopbackHttp)) { $problems.Add("optionele PUBLIC_JWT_ISSUER is ongeldig") }
    if ($env.ContainsKey("PUBLIC_JWT_AUDIENCE") -and -not [string]::IsNullOrWhiteSpace([string]$env["PUBLIC_JWT_AUDIENCE"]) -and (Test-IsPlaceholder $env["PUBLIC_JWT_AUDIENCE"])) { $problems.Add("optionele PUBLIC_JWT_AUDIENCE is placeholder") }
    Add-ContractResult "Public frontend" $snapshot $problems 2
    return $env
}

function Test-AuthEnvironment {
    param([string]$Root)
    $snapshot = Get-EnvironmentSnapshot @((Join-Path $Root ".env"), (Join-Path $Root ".env.local"))
    $env = $snapshot.Values
    $problems = [Collections.Generic.List[string]]::new()
    if (-not $env.ContainsKey("DATABASE_URL") -or -not (Test-ServiceUrl $env["DATABASE_URL"] @("postgres", "postgresql"))) { $problems.Add("DATABASE_URL ontbreekt/is ongeldig") }
    if (-not $env.ContainsKey("REDIS_URL") -or -not (Test-ServiceUrl $env["REDIS_URL"] @("redis", "rediss"))) { $problems.Add("REDIS_URL ontbreekt/is ongeldig") }
    $secureEnvironment = -not $env.ContainsKey("APP_ENV") -or [string]$env["APP_ENV"] -in @("production", "staging")
    if (-not $env.ContainsKey("APP_URL") -or -not (Test-AbsoluteUrl $env["APP_URL"] @("http", "https") -AllowLoopbackHttp)) { $problems.Add("APP_URL ontbreekt/is ongeldig") }
    elseif ($secureEnvironment -and ([Uri]$env["APP_URL"]).Scheme -ne "https") { $problems.Add("APP_URL moet HTTPS zijn voor staging/productie") }
    $rsaIssue = if ($env.ContainsKey("JWT_PRIVATE_KEY")) { Get-RsaPrivateKeyIssue $env["JWT_PRIVATE_KEY"] } else { "ontbreekt" }
    if ($rsaIssue) { $problems.Add("JWT_PRIVATE_KEY $rsaIssue") }
    $tenantKeys = [Collections.Generic.List[string]]::new()
    foreach ($name in @("TENANT_SECRET_KEY", "TENANT_SECRET_KEY_V2", "TENANT_SECRET_KEY_V3")) {
        if (-not $env.ContainsKey($name) -or [string]::IsNullOrWhiteSpace([string]$env[$name])) { continue }
        if ([string]$env[$name] -notmatch '^[0-9A-Fa-f]{64}$') { $problems.Add("$name moet exact 64 hextekens zijn") }
        else { $tenantKeys.Add([string]$env[$name]) }
    }
    if ($tenantKeys.Count -eq 0) { $problems.Add("minstens één TENANT_SECRET_KEY-versie ontbreekt") }
    elseif (@($tenantKeys | Select-Object -Unique).Count -ne $tenantKeys.Count) { $problems.Add("tenant-keyversies mogen geen identieke waarden delen") }
    if (-not $env.ContainsKey("HOMEAPP_LAVENTECARE_INTAKE_SECRET") -or -not (Test-SecretShape $env["HOMEAPP_LAVENTECARE_INTAKE_SECRET"])) { $problems.Add("HOMEAPP_LAVENTECARE_INTAKE_SECRET ontbreekt/is zwak") }
    Add-ContractResult "Auth" $snapshot $problems 6
    return $env
}
function Test-GitRepository {
    param([string]$Label, [string]$Path)

    if (-not (Get-CommandAvailable "git")) { Add-Result "FAIL" "$Label Git" "git is niet geïnstalleerd"; return }
    if (-not (Test-Path -LiteralPath (Join-Path $Path ".git"))) { Add-Result "FAIL" "$Label Git" "repository ontbreekt"; return }

    $branch = (& git -C $Path branch --show-current 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { Add-Result "FAIL" "$Label Git" "repository kan niet worden gelezen"; return }
    $status = @(& git -C $Path status --porcelain=v1 --untracked-files=normal 2>$null)
    if ($LASTEXITCODE -ne 0) { Add-Result "FAIL" "$Label Git" "status kan niet worden gelezen"; return }
    if ([string]::IsNullOrWhiteSpace($branch)) { Add-Result "UNKNOWN" "$Label Git" "detached HEAD; releasebranch is niet vast te stellen"; return }

    if ($status.Count -eq 0) { Add-Result "PASS" "$Label Git" "branch $branch is schoon" }
    else {
        $staged = @($status | Where-Object { $_.Length -ge 2 -and $_[0] -notin @(" ", "?") }).Count
        $untracked = @($status | Where-Object { $_.StartsWith("??", [StringComparison]::Ordinal) }).Count
        Add-Result "FAIL" "$Label Git" "branch $branch is niet schoon: $($status.Count) wijzigingen ($staged staged, $untracked untracked)"
    }
}

function Test-SecretRelationships {
    param([hashtable]$BackendEnv, [hashtable]$HomeappEnv, [hashtable]$AuthEnv)

    $valid = @{}
    foreach ($name in @("APP_SECRET_KEY", "BRIDGE_API_KEY", "LAVENTECARE_SECRET_KEY", "LAVENTECARE_INTAKE_SECRET")) {
        if ($BackendEnv.ContainsKey($name) -and (Test-SecretShape $BackendEnv[$name])) { $valid[$name] = [string]$BackendEnv[$name] }
    }
    if ($valid.Count -lt 2) {
        Add-Result "UNKNOWN" "Secret-scheiding" "minder dan twee geldige lokale waarden; scheiding is niet beoordeelbaar"
    }
    else {
        $duplicates = [Collections.Generic.List[string]]::new()
        $names = @($valid.Keys)
        for ($i = 0; $i -lt $names.Count; $i++) {
            for ($j = $i + 1; $j -lt $names.Count; $j++) {
                if ($valid[$names[$i]] -ceq $valid[$names[$j]]) { $duplicates.Add("$($names[$i])=$($names[$j])") }
            }
        }
        if ($duplicates.Count -gt 0) { Add-Result "FAIL" "Secret-scheiding" "hergebruik gevonden: $($duplicates -join ', ')" }
        else { Add-Result "PASS" "Secret-scheiding" "$($valid.Count) geldige lokale secrets zijn onderling verschillend" }
    }

    if ($HomeappEnv.ContainsKey("BACKEND_API_KEY") -and $BackendEnv.ContainsKey("APP_SECRET_KEY") -and (Test-SecretShape $HomeappEnv["BACKEND_API_KEY"]) -and (Test-SecretShape $BackendEnv["APP_SECRET_KEY"])) {
        if ($HomeappEnv["BACKEND_API_KEY"] -ceq $BackendEnv["APP_SECRET_KEY"]) { Add-Result "PASS" "Homeapp -> Backend trust" "lokale waarden matchen; waarden niet getoond" }
        else { Add-Result "FAIL" "Homeapp -> Backend trust" "lokale geldige waarden matchen niet" }
    }
    else { Add-Result "UNKNOWN" "Homeapp -> Backend trust" "niet genoeg geldige lokale waarden om te vergelijken" }

    if ($BackendEnv.ContainsKey("LAVENTECARE_INTAKE_SECRET") -and $AuthEnv.ContainsKey("HOMEAPP_LAVENTECARE_INTAKE_SECRET") -and (Test-SecretShape $BackendEnv["LAVENTECARE_INTAKE_SECRET"]) -and (Test-SecretShape $AuthEnv["HOMEAPP_LAVENTECARE_INTAKE_SECRET"])) {
        if ($BackendEnv["LAVENTECARE_INTAKE_SECRET"] -ceq $AuthEnv["HOMEAPP_LAVENTECARE_INTAKE_SECRET"]) { Add-Result "PASS" "Auth -> Backend intake" "lokale waarden matchen; waarden niet getoond" }
        else { Add-Result "FAIL" "Auth -> Backend intake" "lokale geldige waarden matchen niet" }
    }
    else { Add-Result "UNKNOWN" "Auth -> Backend intake" "niet genoeg geldige lokale waarden om te vergelijken" }
}
function Invoke-HttpRequestSafe {
    param([string]$Url, [string]$Method = "GET", [hashtable]$Headers = @{})
    try {
        $response = Invoke-WebRequest -Uri $Url -Method $Method -Headers $Headers -MaximumRedirection 5 -TimeoutSec $NetworkTimeoutSec -SkipHttpErrorCheck -UseBasicParsing
        return [pscustomobject]@{ Ok = $true; Response = $response; Reason = "" }
    }
    catch { return [pscustomobject]@{ Ok = $false; Response = $null; Reason = $_.Exception.Message } }
}

function Get-FinalResponseUri {
    param($Response)
    if ($Response.BaseResponse -and $Response.BaseResponse.RequestMessage) { return $Response.BaseResponse.RequestMessage.RequestUri }
    return $null
}

function Test-JsonHealthEndpoint {
    param([string]$Name, [string]$Url)
    $result = Invoke-HttpRequestSafe $Url
    if (-not $result.Ok) { Add-Result "UNKNOWN" $Name $result.Reason; return }
    if ([int]$result.Response.StatusCode -ne 200) { Add-Result "FAIL" $Name "HTTP $([int]$result.Response.StatusCode), verwacht 200"; return }
    try { $json = $result.Response.Content | ConvertFrom-Json -Depth 20 }
    catch { Add-Result "FAIL" $Name "response is geen geldige JSON"; return }
    if ([string]$json.status -notin @("ok", "healthy", "up")) { Add-Result "FAIL" $Name "JSON-status is niet ok/healthy/up"; return }
    Add-Result "PASS" $Name "HTTP 200 met geldige health-status"
}

function Test-OidcDiscovery {
    param([string]$Issuer)
    $result = Invoke-HttpRequestSafe "$($Issuer.TrimEnd('/'))/.well-known/openid-configuration"
    if (-not $result.Ok) { Add-Result "UNKNOWN" "Auth OIDC" $result.Reason; return }
    if ([int]$result.Response.StatusCode -ne 200) { Add-Result "FAIL" "Auth OIDC" "HTTP $([int]$result.Response.StatusCode), verwacht 200"; return }
    try { $json = $result.Response.Content | ConvertFrom-Json -Depth 20 }
    catch { Add-Result "FAIL" "Auth OIDC" "discovery-response is geen geldige JSON"; return }
    $problems = [Collections.Generic.List[string]]::new()
    if ([string]$json.issuer -ne $Issuer.TrimEnd('/')) { $problems.Add("issuer wijkt af") }
    foreach ($name in @("jwks_uri", "authorization_endpoint", "token_endpoint")) {
        if (-not ($json.PSObject.Properties.Name -contains $name) -or -not (Test-AbsoluteUrl ([string]$json.$name) @("https"))) { $problems.Add("$name ontbreekt/is niet HTTPS") }
    }
    if ($problems.Count -gt 0) { Add-Result "FAIL" "Auth OIDC" ($problems -join '; ') }
    else { Add-Result "PASS" "Auth OIDC" "issuer en HTTPS-endpoints zijn coherent" }
}

function Test-JwksEndpoint {
    param([string]$Url)
    $result = Invoke-HttpRequestSafe $Url
    if (-not $result.Ok) { Add-Result "UNKNOWN" "Auth JWKS" $result.Reason; return }
    if ([int]$result.Response.StatusCode -ne 200) { Add-Result "FAIL" "Auth JWKS" "HTTP $([int]$result.Response.StatusCode), verwacht 200"; return }
    try { $json = $result.Response.Content | ConvertFrom-Json -Depth 20 }
    catch { Add-Result "FAIL" "Auth JWKS" "response is geen geldige JSON"; return }
    $keys = @($json.keys)
    if ($keys.Count -eq 0) { Add-Result "FAIL" "Auth JWKS" "keys-array is leeg"; return }
    foreach ($key in $keys) {
        if ([string]$key.kty -ne "RSA" -or [string]::IsNullOrWhiteSpace([string]$key.kid) -or [string]::IsNullOrWhiteSpace([string]$key.n) -or [string]::IsNullOrWhiteSpace([string]$key.e)) { Add-Result "FAIL" "Auth JWKS" "publieke RSA-key mist kty/kid/n/e"; return }
        if ($key.PSObject.Properties.Name | Where-Object { $_ -in @("d", "p", "q", "dp", "dq", "qi") }) { Add-Result "FAIL" "Auth JWKS" "JWKS bevat private keyvelden"; return }
    }
    Add-Result "PASS" "Auth JWKS" "$($keys.Count) publieke RSA-key(s), geen private velden"
}

function Test-WebEndpoint {
    param([string]$Name, [string]$Url, [string[]]$AllowedFinalHosts)
    $result = Invoke-HttpRequestSafe $Url
    if (-not $result.Ok) { Add-Result "UNKNOWN" $Name $result.Reason; return }
    $status = [int]$result.Response.StatusCode
    $final = Get-FinalResponseUri $result.Response
    if ($status -ne 200) { Add-Result "FAIL" $Name "HTTP $status, verwacht 200"; return }
    if (-not $final -or $final.Scheme -ne "https" -or $AllowedFinalHosts -notcontains $final.Host) { Add-Result "FAIL" $Name "redirect eindigt niet op een toegestane HTTPS-host"; return }
    Add-Result "PASS" $Name "HTTP 200; redirect eindigt op toegestane HTTPS-host"
}
function Test-CorsOrigin {
    param([string]$BaseUrl, [string]$Origin)
    $url = "$($BaseUrl.TrimEnd('/'))/public/contact"
    $headers = @{ Origin = $Origin; "Access-Control-Request-Method" = "POST"; "Access-Control-Request-Headers" = "content-type,x-request-id" }
    $positive = Invoke-HttpRequestSafe $url "OPTIONS" $headers
    if (-not $positive.Ok) { Add-Result "UNKNOWN" "CORS $Origin" $positive.Reason }
    else {
        $response = $positive.Response
        $allowOrigin = [string]$response.Headers["Access-Control-Allow-Origin"]
        $allowMethods = [string]$response.Headers["Access-Control-Allow-Methods"]
        $allowHeaders = [string]$response.Headers["Access-Control-Allow-Headers"]
        $vary = [string]$response.Headers["Vary"]
        $valid = [int]$response.StatusCode -in @(200, 204) -and $allowOrigin -eq $Origin -and $allowMethods -match '(?i)(^|,|\s)POST($|,|\s)' -and $allowHeaders -match '(?i)content-type' -and $allowHeaders -match '(?i)x-request-id' -and $vary -match '(?i)(^|,|\s)Origin($|,|\s)'
        if ($valid) { Add-Result "PASS" "CORS $Origin" "exacte origin, POST/headers en Vary: Origin bevestigd" }
        else { Add-Result "FAIL" "CORS $Origin" "preflight mist exacte origin, methode/headers of Vary: Origin" }
    }

    $evilOrigin = "https://readiness-denied.invalid"
    $negative = Invoke-HttpRequestSafe $url "OPTIONS" @{ Origin = $evilOrigin; "Access-Control-Request-Method" = "POST" }
    if (-not $negative.Ok) { Add-Result "UNKNOWN" "CORS deny $Origin" $negative.Reason; return }
    $deniedAllowOrigin = [string]$negative.Response.Headers["Access-Control-Allow-Origin"]
    if ($deniedAllowOrigin -eq "*" -or $deniedAllowOrigin -eq $evilOrigin) { Add-Result "FAIL" "CORS deny $Origin" "niet-toegestane origin kreeg CORS-toegang" }
    else { Add-Result "PASS" "CORS deny $Origin" "niet-toegestane origin kreeg geen allow-origin" }
}
function Test-TlsCertificate {
    param([string]$HostName)
    $client = $null
    $stream = $null
    $cancellation = $null
    try {
        $cancellation = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($NetworkTimeoutSec))
        $client = [Net.Sockets.TcpClient]::new()
        $client.ConnectAsync($HostName, 443, $cancellation.Token).GetAwaiter().GetResult()
        $stream = [Net.Security.SslStream]::new($client.GetStream(), $false)
        $options = [Net.Security.SslClientAuthenticationOptions]::new()
        $options.TargetHost = $HostName
        $options.CertificateRevocationCheckMode = [Security.Cryptography.X509Certificates.X509RevocationMode]::Online
        $stream.AuthenticateAsClientAsync($options, $cancellation.Token).GetAwaiter().GetResult()
        $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new($stream.RemoteCertificate)
        $now = [DateTime]::UtcNow
        if ($certificate.NotBefore.ToUniversalTime() -gt $now -or $certificate.NotAfter.ToUniversalTime() -le $now) { Add-Result "FAIL" "TLS $HostName" "certificaat is nog niet geldig of verlopen"; return }
        $days = [math]::Floor(($certificate.NotAfter.ToUniversalTime() - $now).TotalDays)
        if ($days -lt 14) { Add-Result "UNKNOWN" "TLS $HostName" "certificaat verloopt binnen 14 dagen ($days dagen)" }
        else { Add-Result "PASS" "TLS $HostName" "handshake en hostname-validatie geslaagd; nog $days dagen" }
    }
    catch [OperationCanceledException] { Add-Result "UNKNOWN" "TLS $HostName" "handshake-timeout na $NetworkTimeoutSec seconden" }
    catch { Add-Result "FAIL" "TLS $HostName" "TLS-handshake of certificaatvalidatie faalde" }
    finally {
        if ($stream) { $stream.Dispose() }
        if ($client) { $client.Dispose() }
        if ($cancellation) { $cancellation.Dispose() }
    }
}

function Invoke-ProcessCapture {
    param([string]$FilePath, [string[]]$Arguments, [string]$WorkingDirectory, [int]$TimeoutSec)
    $startInfo = [Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $FilePath
    $startInfo.WorkingDirectory = $WorkingDirectory
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $null = $startInfo.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    $null = $process.Start()
    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    $timedOut = -not $process.WaitForExit($TimeoutSec * 1000)
    if ($timedOut) { try { $process.Kill($true) } catch {}; $process.WaitForExit() }
    $output = $stdoutTask.Result
    $null = $stderrTask.Result
    $exitCode = if ($timedOut) { -1 } else { $process.ExitCode }
    $process.Dispose()
    return [pscustomobject]@{ ExitCode = $exitCode; StdOut = $output; TimedOut = $timedOut }
}

function Invoke-VercelJson {
    param([string[]]$Arguments, [string]$HomeappRoot)
    $vercel = Get-Command "vercel.cmd" -ErrorAction SilentlyContinue
    if (-not $vercel) { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI ontbreekt"; Data = $null } }
    if ($vercel.Source -match '[\r\n"&|<>^%!]') { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI-pad is niet veilig uitvoerbaar"; Data = $null } }
    foreach ($argument in $Arguments) {
        if ($argument -match '[\r\n"&|<>^%!]') { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI-argument is niet veilig uitvoerbaar"; Data = $null } }
    }
    $quotedArguments = @($Arguments | ForEach-Object { '"' + $_ + '"' }) -join ' '
    $commandLine = 'call "' + $vercel.Source + '" ' + $quotedArguments
    $result = Invoke-ProcessCapture $env:ComSpec @("/d", "/s", "/c", $commandLine) $HomeappRoot $NetworkTimeoutSec
    if ($result.TimedOut) { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI timeout na $NetworkTimeoutSec seconden"; Data = $null } }
    if ($result.ExitCode -ne 0) { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI exitcode $($result.ExitCode); login/netwerk controleren"; Data = $null } }
    try { return [pscustomobject]@{ Ok = $true; Reason = ""; Data = ($result.StdOut | ConvertFrom-Json -Depth 30) } }
    catch { return [pscustomobject]@{ Ok = $false; Reason = "Vercel CLI gaf geen geldige JSON"; Data = $null } }
}
function Test-VercelProjectLink {
    param([string]$HomeappRoot)
    $path = Join-Path $HomeappRoot ".vercel/project.json"
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { Add-Result "FAIL" "Vercel projectlink" ".vercel/project.json ontbreekt"; return $false }
    try { $link = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json }
    catch { Add-Result "FAIL" "Vercel projectlink" "project.json is ongeldig"; return $false }
    if ($link.projectId -ne $script:VercelProjectId -or $link.orgId -ne $script:VercelTeamId) { Add-Result "FAIL" "Vercel projectlink" "gekoppeld project/team wijkt af van de vastgezette IDs"; return $false }
    Add-Result "PASS" "Vercel projectlink" "project- en team-ID matchen de vastgezette Homeapp-koppeling"
    return $true
}

function Test-VercelEnvironmentNames {
    param([string]$HomeappRoot)
    if (-not (Test-VercelProjectLink $HomeappRoot)) { return }
    $required = @("NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY", "CLERK_SECRET_KEY", "BACKEND_API_URL", "BACKEND_API_KEY", "HOMEAPP_OWNER_USER_ID")
    foreach ($environment in @("preview", "production")) {
        $result = Invoke-VercelJson @("env", "ls", $environment, "--format", "json", "--non-interactive", "--no-color", "--scope", $script:VercelScope) $HomeappRoot
        if (-not $result.Ok) { Add-Result "UNKNOWN" "Vercel $environment env" $result.Reason; continue }
        $names = @($result.Data.envs | ForEach-Object { [string]$_.key })
        $missing = @($required | Where-Object { $names -notcontains $_ })
        if ($missing.Count -gt 0) { Add-Result "FAIL" "Vercel $environment env" "ontbrekende runtime-namen: $($missing -join ', ')" }
        else { Add-Result "PASS" "Vercel $environment env" "alle vijf runtime-namen aanwezig; waarden niet opgehaald" }
    }
}

function Test-VercelDeployments {
    param([string]$HomeappRoot)
    $localSha = (& git -C $HomeappRoot rev-parse HEAD 2>$null | Out-String).Trim().ToLowerInvariant()
    $branch = (& git -C $HomeappRoot branch --show-current 2>$null | Out-String).Trim()
    $sha = if ([string]::IsNullOrWhiteSpace($IntendedHomeappSha)) { $localSha } else { $IntendedHomeappSha.Trim().ToLowerInvariant() }
    if ($sha -notmatch '^[0-9a-f]{40}$') { Add-Result "FAIL" "Vercel intended SHA" "bedoelde SHA of lokale HEAD is geen volledige Git SHA"; return }
    $base = @($script:VercelProjectName, "--limit", "1", "--format", "json", "--yes", "--non-interactive", "--no-color", "--scope", $script:VercelScope)
    $production = Invoke-VercelJson (@("list") + $base + @("--environment", "production")) $HomeappRoot
    $productionMatches = $false
    if (-not $production.Ok) { Add-Result "UNKNOWN" "Vercel production SHA" $production.Reason }
    else {
        $deployment = @($production.Data.deployments) | Select-Object -First 1
        if (-not $deployment) { Add-Result "UNKNOWN" "Vercel production SHA" "geen productiedeployment gevonden" }
        elseif ([string]$deployment.state -ne "READY") { Add-Result "FAIL" "Vercel production SHA" "nieuwste productiedeployment is $([string]$deployment.state), niet READY" }
        elseif ([string]::IsNullOrWhiteSpace([string]$deployment.meta.githubCommitSha)) { Add-Result "UNKNOWN" "Vercel production SHA" "deploymentmetadata bevat geen Git SHA" }
        elseif ([string]$deployment.meta.githubCommitSha -eq $sha) { $productionMatches = $true; Add-Result "PASS" "Vercel production SHA" "READY-deployment matcht de bedoelde SHA" }
        elseif ($branch -in @("main", "master") -and $sha -eq $localSha) { Add-Result "FAIL" "Vercel production SHA" "productie wijkt af van HEAD op releasebranch $branch" }
        else { Add-Result "INFO" "Vercel production SHA" "productie draait een andere SHA; voor featurebranch $branch is preview doorslaggevend" }
    }

    $preview = Invoke-VercelJson (@("list") + $base + @("--environment", "preview", "--meta", "githubCommitSha=$sha")) $HomeappRoot
    if (-not $preview.Ok) {
        if ($productionMatches) { Add-Result "INFO" "Vercel preview SHA" "productie matcht; preview kon niet worden beoordeeld" }
        else { Add-Result "UNKNOWN" "Vercel preview SHA" $preview.Reason }
        return
    }
    $deployment = @($preview.Data.deployments) | Select-Object -First 1
    if (-not $deployment) {
        if ($productionMatches) { Add-Result "INFO" "Vercel preview SHA" "geen preview nodig omdat productie de bedoelde SHA draait" }
        else { Add-Result "UNKNOWN" "Vercel preview SHA" "geen previewdeployment voor de bedoelde SHA gevonden" }
    }
    elseif ([string]$deployment.state -eq "READY" -and [string]$deployment.meta.githubCommitSha -eq $sha) { Add-Result "PASS" "Vercel preview SHA" "READY-preview matcht de bedoelde SHA" }
    elseif ([string]$deployment.state -in @("ERROR", "CANCELED")) { Add-Result "FAIL" "Vercel preview SHA" "preview voor de bedoelde SHA is $([string]$deployment.state)" }
    else { Add-Result "UNKNOWN" "Vercel preview SHA" "preview voor de bedoelde SHA is nog $([string]$deployment.state)" }
}
function Test-AuthMigrationFiles {
    param([string]$AuthRoot)
    $problems = [Collections.Generic.List[string]]::new()
    foreach ($version in @("20260717000001_public_contact_idempotency", "20260717000002_laventecare_com_origins", "20260717000003_email_logs_sent_at")) {
        foreach ($suffix in @(".up.sql", ".down.sql")) {
            $relative = "migrations/$version$suffix"
            if (-not (Test-Path -LiteralPath (Join-Path $AuthRoot $relative) -PathType Leaf)) { $problems.Add("$relative ontbreekt"); continue }
            & git -C $AuthRoot cat-file -e "HEAD`:$relative" 2>$null
            if ($LASTEXITCODE -ne 0) { $problems.Add("$relative staat niet in HEAD"); continue }
            $status = @(& git -C $AuthRoot status --porcelain=v1 -- $relative 2>$null)
            if ($LASTEXITCODE -ne 0 -or $status.Count -gt 0) { $problems.Add("$relative wijkt lokaal af van HEAD") }
        }
    }
    if ($problems.Count -gt 0) { Add-Result "FAIL" "Auth-migraties" ($problems -join '; ') }
    else { Add-Result "PASS" "Auth-migraties" "alle zes bestanden bestaan ongewijzigd in HEAD" }
}

function Test-KnownSecretArtifacts {
    param([string]$AuthRoot)
    $dump = Join-Path $AuthRoot "backup_before_email_security_20260202_183058.sql"
    if (Test-Path -LiteralPath $dump -PathType Leaf) { Add-Result "FAIL" "Auth secret-artifact" "gevoelige databaseback-up staat nog in de werkboom" }
    else { Add-Result "PASS" "Auth secret-artifact" "bekende gevoelige databaseback-up ontbreekt uit de werkboom" }

    $cryptoFile = Join-Path $AuthRoot "internal/crypto/tenant_secrets.go"
    if (-not (Test-Path -LiteralPath $cryptoFile -PathType Leaf)) { Add-Result "FAIL" "Tenant-key broncontrole" "tenant crypto-bestand ontbreekt" }
    else {
        $crypto = Get-Content -LiteralPath $cryptoFile -Raw
        if ($crypto -match 'os\.Setenv|os\.Unsetenv') { Add-Result "FAIL" "Tenant-key broncontrole" "decryptie muteert procesglobale environment" }
        else { Add-Result "INFO" "Tenant-key broncontrole" "geen procesglobale env-mutatie gevonden; dit bewijst geen data-rotatie" }
    }
    Add-Result "UNKNOWN" "Tenant V1-verwijdering" "geblokkeerd totdat productie-inventaris en herencryptie aantonen dat geen V1-ciphertext resteert"
}
function Invoke-ReadOnlyCheck {
    $backendRoot = Split-Path $PSScriptRoot -Parent
    $projectsRoot = Split-Path $backendRoot -Parent
    $repos = [ordered]@{
        Backend = $backendRoot
        Homeapp = Join-Path $projectsRoot "JeffriesHomeapp"
        Public = Join-Path $projectsRoot "LaventeCareFrontend"
        Auth = Join-Path $projectsRoot "LaventeCareAuthSystems"
    }

    Write-Host ""
    Write-Host "Jeffries/LaventeCare production readiness (read-only)" -ForegroundColor Cyan
    Write-Host "Geen environmentwaarden worden getoond. UNKNOWN maakt exitcode 2." -ForegroundColor DarkGray
    Write-Host ""
    foreach ($entry in $repos.GetEnumerator()) { Test-GitRepository $entry.Key $entry.Value }

    $backendEnv = Test-BackendEnvironment $repos.Backend
    $homeappEnv = Test-HomeappEnvironment $repos.Homeapp
    $null = Test-PublicEnvironment $repos.Public
    $authEnv = Test-AuthEnvironment $repos.Auth
    Test-SecretRelationships $backendEnv $homeappEnv $authEnv
    Test-AuthMigrationFiles $repos.Auth
    Test-KnownSecretArtifacts $repos.Auth

    if ($SkipNetwork) {
        Add-Result "INFO" "Netwerkchecks" "overgeslagen via -SkipNetwork; Vercel/deploy/HTTP/TLS zijn niet beoordeeld"
        return
    }

    Test-VercelEnvironmentNames $repos.Homeapp
    Test-VercelDeployments $repos.Homeapp
    Test-JsonHealthEndpoint "Backend health" "https://jeffriesbackend.onrender.com/api/v1/health"
    Test-JsonHealthEndpoint "Auth health" "https://laventecareauthsystems.onrender.com/health"
    Test-OidcDiscovery "https://laventecareauthsystems.onrender.com"
    Test-JwksEndpoint "https://laventecareauthsystems.onrender.com/.well-known/jwks.json"
    Test-WebEndpoint "LaventeCare NL" "https://laventecare.nl" @("laventecare.nl", "www.laventecare.nl")
    Test-WebEndpoint "LaventeCare COM" "https://laventecare.com" @("laventecare.com", "www.laventecare.com")
    Test-WebEndpoint "Homeapp production" "https://jeffries-homeapp.vercel.app" @("jeffries-homeapp.vercel.app")
    Test-CorsOrigin "https://laventecareauthsystems.onrender.com" "https://laventecare.nl"
    Test-CorsOrigin "https://laventecareauthsystems.onrender.com" "https://laventecare.com"
    foreach ($hostName in @("jeffriesbackend.onrender.com", "laventecareauthsystems.onrender.com", "laventecare.nl", "laventecare.com", "jeffries-homeapp.vercel.app")) {
        Test-TlsCertificate $hostName
    }
}
function Get-FreeLoopbackPort {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    try { $listener.Start(); return ([Net.IPEndPoint]$listener.LocalEndpoint).Port }
    finally { $listener.Stop() }
}

function Stop-OwnedProcessTree {
    param([Diagnostics.Process]$Process)
    if (-not $Process -or $Process.HasExited) { return $true }
    try {
        & "$env:SystemRoot\System32\taskkill.exe" /PID $Process.Id /T /F *> $null
        return $LASTEXITCODE -eq 0
    }
    catch { return $false }
}

function Invoke-HomeappAuthenticatedE2E {
    $backendRoot = Split-Path $PSScriptRoot -Parent
    $homeappRoot = Join-Path (Split-Path $backendRoot -Parent) "JeffriesHomeapp"
    if (-not (Test-Path -LiteralPath (Join-Path $homeappRoot "package.json") -PathType Leaf)) { Add-Result "FAIL" "Homeapp authenticated E2E" "Homeapp-repository ontbreekt"; return }
    if (-not (Get-CommandAvailable "npm.cmd") -or -not (Get-CommandAvailable "npx.cmd")) { Add-Result "FAIL" "Homeapp authenticated E2E" "npm.cmd en npx.cmd zijn vereist"; return }

    $resolvedState = [IO.Path]::GetFullPath($AuthStatePath)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedState.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -or [IO.Path]::GetExtension($resolvedState) -ne ".json") { Add-Result "FAIL" "Homeapp authenticated E2E" "auth-state moet een .json-bestand binnen de OS-tempdirectory zijn"; return }
    if (Test-Path -LiteralPath $resolvedState) {
        $existing = Get-Item -LiteralPath $resolvedState -Force
        if ($existing.Attributes.HasFlag([IO.FileAttributes]::ReparsePoint)) { Add-Result "FAIL" "Homeapp authenticated E2E" "bestaand auth-statepad is een reparse point"; return }
        if (-not $OverwriteAuthState) { Add-Result "FAIL" "Homeapp authenticated E2E" "auth-state bestaat al; gebruik expliciet -OverwriteAuthState of kies een ander temp-pad"; return }
        Remove-Item -LiteralPath $resolvedState -Force
    }

    $port = Get-FreeLoopbackPort
    $baseUrl = "http://127.0.0.1:$port"
    $workTemp = Join-Path $tempRoot ("jeffries-homeapp-e2e-" + [guid]::NewGuid().ToString("N"))
    $null = New-Item -ItemType Directory -Path $workTemp
    $stdoutLog = Join-Path $workTemp "dev.stdout.log"
    $stderrLog = Join-Path $workTemp "dev.stderr.log"
    $server = $null
    $succeeded = $false
    $cleanupFailed = $false
    $oldAuthState = $env:E2E_AUTH_STATE
    $oldBaseUrl = $env:E2E_BASE_URL
    $oldExternalServer = $env:E2E_EXTERNAL_SERVER
    $oldLastRunFile = $env:PLAYWRIGHT_LAST_RUN_OUTPUT_FILE

    try {
        $server = Start-Process -FilePath "npm.cmd" -ArgumentList @("run", "dev", "--", "--hostname", "127.0.0.1", "--port", [string]$port) -WorkingDirectory $homeappRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog
        $ready = $false
        for ($attempt = 0; $attempt -lt 90; $attempt++) {
            Start-Sleep -Seconds 1
            if ($server.HasExited) { throw "dev-server stopte voortijdig" }
            try {
                $probe = Invoke-WebRequest -Uri "$baseUrl/sign-in" -TimeoutSec 2 -MaximumRedirection 3 -SkipHttpErrorCheck -UseBasicParsing
                $final = Get-FinalResponseUri $probe
                if ([int]$probe.StatusCode -lt 500 -and $final.Host -eq "127.0.0.1" -and $final.Port -eq $port -and $probe.Content -match 'Jeffries Dashboard') { $ready = $true; break }
            }
            catch {}
        }
        if (-not $ready) { throw "eigen Homeapp-marker werd niet binnen 90 seconden op de loopback-poort gevonden" }

        Write-Host ""
        Write-Host "Er opent nu een gewone Playwright-browser (geen codegen/actie-opname)." -ForegroundColor Cyan
        Write-Host "Log éénmalig in als Homeapp-owner en sluit daarna het browservenster."
        Write-Host "Auth-state blijft uitsluitend in de OS-tempdirectory."
        Write-Host ""

        Push-Location $homeappRoot
        try {
            & npx.cmd --no-install playwright open --browser chromium "--save-storage=$resolvedState" "$baseUrl/sign-in"
            if ($LASTEXITCODE -ne 0) { throw "Playwright open eindigde met exitcode $LASTEXITCODE" }
            if (-not (Test-Path -LiteralPath $resolvedState -PathType Leaf)) { throw "Playwright maakte geen auth-statebestand" }
            $stateInfo = Get-Item -LiteralPath $resolvedState -Force
            if ($stateInfo.Attributes.HasFlag([IO.FileAttributes]::ReparsePoint) -or $stateInfo.Length -le 2 -or $stateInfo.Length -gt 5MB) { throw "auth-statebestand heeft een onveilige vorm of omvang" }
            try { $state = Get-Content -LiteralPath $resolvedState -Raw | ConvertFrom-Json -Depth 30 }
            catch { throw "auth-statebestand is geen geldige JSON" }
            if (-not ($state.PSObject.Properties.Name -contains "cookies") -or -not ($state.PSObject.Properties.Name -contains "origins")) { throw "auth-state mist Playwright cookies/origins" }

            $env:E2E_AUTH_STATE = $resolvedState
            $env:E2E_BASE_URL = $baseUrl
            $env:E2E_EXTERNAL_SERVER = "1"
            $env:PLAYWRIGHT_LAST_RUN_OUTPUT_FILE = Join-Path $workTemp "last-run.json"
            & npx.cmd --no-install playwright test --project=chromium --reporter=line --workers=1 --output (Join-Path $workTemp "test-results")
            if ($LASTEXITCODE -ne 0) { throw "authenticated E2E faalde met exitcode $LASTEXITCODE" }
        }
        finally { Pop-Location }

        $succeeded = $true
        Add-Result "PASS" "Homeapp authenticated E2E" "suite geslaagd op eigen 127.0.0.1-origin"
    }
    catch { Add-Result "FAIL" "Homeapp authenticated E2E" "$($_.Exception.Message). Diagnostiek bewaard in $workTemp" }
    finally {
        $env:E2E_AUTH_STATE = $oldAuthState
        $env:E2E_BASE_URL = $oldBaseUrl
        $env:E2E_EXTERNAL_SERVER = $oldExternalServer
        $env:PLAYWRIGHT_LAST_RUN_OUTPUT_FILE = $oldLastRunFile
        if (-not (Stop-OwnedProcessTree $server)) { $cleanupFailed = $true; Add-Result "FAIL" "Homeapp E2E cleanup" "de uitsluitend door deze run gestarte procesboom kon niet worden gestopt" }
        if (-not $KeepAuthState -and (Test-Path -LiteralPath $resolvedState)) {
            try { Remove-Item -LiteralPath $resolvedState -Force }
            catch { $cleanupFailed = $true; Add-Result "FAIL" "Homeapp E2E cleanup" "tijdelijke auth-state kon niet worden verwijderd" }
        }
        if ($succeeded -and -not $cleanupFailed -and (Test-Path -LiteralPath $workTemp)) {
            try { Remove-Item -LiteralPath $workTemp -Recurse -Force }
            catch { Add-Result "UNKNOWN" "Homeapp E2E cleanup" "geslaagde tijdelijke logmap kon niet worden verwijderd: $workTemp" }
        }
    }
}
function Open-ProviderDashboards {
    $urls = @(
        "https://vercel.com/jeffreys-projects-c48521c1/jeffries-homeapp/settings/environment-variables",
        "https://dashboard.render.com/",
        "https://dashboard.clerk.com/",
        "https://app.todoist.com/app/settings/integrations/developer",
        "https://console.cloud.google.com/apis/credentials",
        "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
        "https://console.groq.com/keys",
        "https://console.x.ai/"
    )
    foreach ($url in $urls) { Start-Process -FilePath $url }
    Add-Result "INFO" "Dashboards" "$($urls.Count) providerpagina's geopend; er is niets gewijzigd"
}

try {
    switch ($Mode) {
        "Check" { Invoke-ReadOnlyCheck }
        "HomeappAuthE2E" { Invoke-HomeappAuthenticatedE2E }
        "OpenDashboards" { Open-ProviderDashboards }
    }
}
catch { Add-Result "FAIL" "Readiness helper" "onverwachte lokale fout: $($_.Exception.Message)" }

$passed = @($script:Results | Where-Object State -eq "PASS").Count
$unknown = @($script:Results | Where-Object State -eq "UNKNOWN").Count
$failed = @($script:Results | Where-Object State -eq "FAIL").Count
$info = @($script:Results | Where-Object State -eq "INFO").Count
Write-Host ""
Write-Host "Samenvatting: $passed geslaagd, $unknown onbekend, $failed fouten, $info info" -ForegroundColor Cyan
if ($failed -gt 0) { exit 1 }
if ($unknown -gt 0) { exit 2 }
exit 0
