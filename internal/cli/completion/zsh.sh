#compdef bootwright
_bootwright_complete() {
    local response line candidate description directive=0
    local -a candidates descriptions options
    response=$("${words[1]}" @PROTOCOL@ "${words[@]:1:$((CURRENT-1))}" 2>/dev/null)
    for line in "${(@f)response}"; do
        if [[ $line == :* ]]; then
            directive=${line#:}
            continue
        fi
        candidate=${line%%$'\t'*}
        description=${line#*$'\t'}
        [[ $description == $line ]] && description=$candidate
        candidates+=("$candidate")
        descriptions+=("$description")
    done
    (( directive & 2 )) && options=(-S "")
    (( ${#candidates} )) && compadd -Q -V bootwright -d descriptions "${options[@]}" -- "${candidates[@]}"
    return 0
}
compdef _bootwright_complete bootwright
