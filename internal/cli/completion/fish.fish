function __bootwright_candidates
    set -l words (commandline -opc)
    set -l current (commandline -ct)
    set -l executable $words[1]
    set -e words[1]
    for line in (command $executable @PROTOCOL@ $words "$current" 2>/dev/null)
        string match -q ':*' -- "$line"; and continue
        printf '%s\n' "$line"
    end
end
complete -c bootwright -e
complete -c bootwright -f -k -a '(__bootwright_candidates)'
