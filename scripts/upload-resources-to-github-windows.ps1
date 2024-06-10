# upload-resources-to-github-windows.ps1

# PowerShell script to upload windows release assets to Github.
# This script cleans up after itself in cases of parital failures. i.e. either all assets are uploaded or none

$USAGE = @'
Usage: upload-resources-to-github-windows.ps1 [-BinariesOnly]
Upload windows release assets to GitHub. Release assets include binaries for supported platforms and K8s resources for supported versions.

Options:
    -BinariesOnly       Upload binaries only 
    -K8sAssetsOnly      Upload only the K8s Assets
    -Suffix <suffix>    String appended to resource file names
'@

function usage {
    Write-Output $USAGE
}

param(
    [switch]$BinariesOnly,
    [switch]$K8sAssetsOnly,
    [switch]$Suffix = ""
)

# Check if no options are provided or invalid options are used
if (-not $BinariesOnly -and -not $K8sAssetsOnly -and $Suffix -eq "") {
    usage
    exit 1
}

$ErrorActionPreference = "Stop"

# Function to handle errors and cleanup any partially uploaded assets
function HandleErrorsAndCleanup {
    param (
        [int]$ExitCode
    )
    if ($ExitCode -eq 0) {
        exit 0
    }
    if ($global:AssetIdsUploaded.Count -ne 0) {
        Write-Output "`nCleaning up assets uploaded in the current execution of the script"
        foreach ($assetId in $global:AssetIdsUploaded) {
            Write-Output "Deleting asset $assetId"
            Invoke-RestMethod -Method Delete -Uri "https://api.github.com/repos/aws/aws-node-termination-handler/releases/assets/$assetId" -Headers @{Authorization = "token $env:GITHUB_TOKEN"}
        }
        exit $ExitCode
    }
}

# Function to upload an asset to GitHub
function UploadAsset {
    param (
        [string]$AssetPath
    )
    $ContentType = [System.Web.MimeMapping]::GetMimeMapping($AssetPath)
    $Headers = @{
        Authorization = "token $env:GITHUB_TOKEN"
        'Content-Type' = $ContentType
    }
    $Uri = "https://uploads.github.com/repos/aws/aws-node-termination-handler/releases/$ReleaseId/assets?name=$(Split-Path -Leaf $AssetPath)"
    
    try {
        $Response = Invoke-RestMethod -Method Post -Uri $Uri -Headers $Headers -InFile $AssetPath -ErrorAction Stop
        if ($Response -and $Response.id) {
            $global:AssetIdsUploaded += $Response.id
            Write-Output "Created asset ID $($Response.id) successfully"
        } else {
            Write-Output "❌ Upload failed with response message: $($Response | ConvertTo-Json) ❌"
            exit 1
        }
    } catch {
        Write-Output "❌ Upload failed for $AssetPath with error: $_"
        exit 1
    }
}

# Initialize global variables
$global:AssetIdsUploaded = @()
trap { HandleErrorsAndCleanup -ExitCode $global:LASTEXITCODE }

$ScriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
$Version = & make -s -f "$ScriptPath/../Makefile" version
$BuildDir = "$ScriptPath/../build/k8s-resources/$Version"
$BinaryDir = "$ScriptPath/../build/bin"

# Set the TLS version, powershell is supported in GitHub actions using Tls
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

try {
    $Response = (Invoke-RestMethod -Uri "https://api.github.com/repos/aws/aws-node-termination-handler/releases" -Headers @{Authorization = "token $env:GITHUB_TOKEN"})
} catch {
    Write-Output "Failed to retrieve releases from GitHub: $_"
    exit 1
}

$release = $Response | Where-Object { $_.tag_name -eq $Version }

Write-Output "Latest Release"
Write-Output $release

$ReleaseId = $release.id
Write-Output "Release ID: $ReleaseId "

if (-not $ReleaseId) {
    Write-Output "❌ Failed to find release ID for version $Version  ❌"
    exit 1
}

# Gather assets to upload
$Assets = @()
if ($BinariesOnly) {
    $Assets += Get-ChildItem -Path $BinaryDir | ForEach-Object { $_.FullName }
}
if (-not $BinariesOnly) {
    $Assets += "$BuildDir\individual-resources.tar", "$BuildDir\all-resources.yaml", "$BuildDir\individual-resources-queue-processor.tar", "$BuildDir\all-resources-queue-processor.yaml"
}

# Upload each asset
Write-Output "`nUploading release assets for release id '$ReleaseId' to Github"
foreach ($Asset in $Assets) {
    Write-Output "`n  Uploading $($Asset | Split-Path -Leaf)"
    UploadAsset -AssetPath $Asset
}
