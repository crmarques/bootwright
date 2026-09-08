#compdef bootwright
_bootwright_complete() {
    local response line candidate description
    local -a candidates descriptions
    response=$("${words[1]}" @PROTOCOL@ "${words[@]:1:$((CURRENT-1))}" 2>/dev/null)
    for line in "${(@f)response}"; do
        [[ $line == :* ]] && continue
        candidate=${line%%$'\t'*}
        description=${line#*$'\t'}
        [[ $description == $line ]] && description=$candidate
        candidates+=("$candidate")
        descriptions+=("$description")
    done
    (( ${#candidates} )) && compadd -Q -V bootwright -d descriptions -- "${candidates[@]}"
    return 0
}
compdef _bootwright_complete bootwright
