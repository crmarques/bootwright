if ($PSVersionTable.PSVersion -lt [System.Management.Automation.SemanticVersion]'7.7.0-preview.2') {
    throw 'Bootwright completion requires PowerShell 7.7.0-preview.2 or newer.'
}
Register-ArgumentCompleter -Native -CommandName bootwright -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $elements = @($commandAst.CommandElements)
    $executable = $elements[0].Value
    $arguments = @()
    foreach ($element in $elements | Select-Object -Skip 1) {
        if ($element.Extent.StartOffset -ge $cursorPosition) { break }
        if ($element.Extent.EndOffset -ge $cursorPosition -and $wordToComplete -ne '') { break }
        if ($element -is [System.Management.Automation.Language.StringConstantExpressionAst]) {
            $arguments += $element.Value
        } else {
            $arguments += $element.Extent.Text
        }
    }
    $arguments += $wordToComplete
    $results = @(& $executable @PROTOCOL@ @arguments 2>$null)
    foreach ($line in $results) {
        if ($line.StartsWith(':')) { continue }
        $parts = $line.Split([char]9, 2)
        $candidate = $parts[0]
        $description = $candidate
        if ($parts.Length -gt 1) { $description = $parts[1] }
        [System.Management.Automation.CompletionResult]::new($candidate, $candidate, 'ParameterValue', $description)
    }
    if ($results.Count -le 1) { '' }
}
