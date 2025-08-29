package aws

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/sso/types"
)

// check erros
func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

type Credential struct {
	AccessKeyId     string
	SecretAccessKey string
	SessionToken    string
}

type Role struct {
	Name        string
	Credentials Credential
	Account     *Account
}

type Account struct {
	Id    string
	Name  string
	Roles []Role
}

type Client struct {
	Client *sso.Client
	Token  Token
}

type Token struct {
	AccessToken string `json:"accessToken"`
}

func (t Token) String() string {
	return fmt.Sprintf("%v", t.AccessToken)
}

// per 429 failure backoff in seconds
var backoffSchedule = []time.Duration{
	10 * time.Second,
	5 * time.Second,
	1 * time.Second,
}

// Get account data minus roles and role credentials
func GetAccounts(verbose bool, accounts []Account, client *Client) []Account {

	if verbose {
		log.Println("getting aws accounts")
	}

	var tooManyRequests429 *types.TooManyRequestsException
	var unauthorizedException401 *types.UnauthorizedException

	inputs := &sso.ListAccountsInput{
		AccessToken: &client.Token.AccessToken,
	}

	// Get the first entry and NextToken
	listAccountOutput, err := client.Client.ListAccounts(context.TODO(), inputs)

	if errors.As(err, &unauthorizedException401) {
		log.Println(err.Error())
		log.Fatal("random unathorized 401 exception, potentially a bug in the aws api or sdk, rerun the create command to resovle.")
	}

	// handle 401 when sso session is timed out
	if listAccountOutput == nil {
		log.Printf("%v\n", err.Error())
		log.Fatal("ensure that sso session has not expired. Use aws sso login --profile=<profile>.")
	}

	for _, account := range listAccountOutput.AccountList {
		accountName, accountId := *account.AccountName, *account.AccountId
		if verbose {
			log.Println("building account state for", accountName, accountId)
		}
		accounts = append(accounts, Account{accountId, accountName, []Role{}})
	}

	inputs.NextToken = listAccountOutput.NextToken

	for listAccountOutput.NextToken != nil {

		// back off for in seconds when recieving 429
		for _, backoff := range backoffSchedule {
			listAccountOutput, err = client.Client.ListAccounts(context.TODO(), inputs)

			if err == nil {
				break
			}

			if errors.As(err, &tooManyRequests429) {
				log.Printf("\nBacking off ListAccounts request rate...\n")
				time.Sleep(backoff)
			}

		}

		// all retries failed
		check(err)
		inputs.NextToken = listAccountOutput.NextToken

		for _, account := range listAccountOutput.AccountList {
			accountName, accountId := *account.AccountName, *account.AccountId
			if verbose {
				log.Println("building account state for", accountName, accountId)
			}
			accounts = append(accounts, Account{accountId, accountName, []Role{}})
		}
	}
	return accounts
}

type Worker func(int, chan Role, *Client, chan Credential, bool, bool)

// break up the account list into blocks by chunkSize
func chunker(verbose bool, s []Account, chunkSize int) [][]Account {

	var chunks [][]Account
	numberOfChunks := len(s) / chunkSize

	start := 0
	end := start + chunkSize

	for i := 1; i <= numberOfChunks; i++ {
		if verbose {
			log.Printf("breaking up accounts into chunks for processing, chunk-%v", i)
		}
		chunks = append(chunks, s[start:end])

		start = end
		end = end + chunkSize
	}
	chunks = append(chunks, s[start:])

	return chunks
}

// starts generic workers of type Worker and processes the role queue
func InterrogateRoles(accounts []Account, client *Client, worker Worker, bSize *int, verbose *bool, refreshCredentials bool, numWorkers *int) []Account {
	if *verbose {
		log.Println("getting roles")
	}
	roleQueue := make(chan Role)
	credentialsQueue := make(chan Credential)
	batchSize := *bSize
	verboseLogging := *verbose
	numberOfWorkers := *numWorkers
	uniqueAccountsProcessed := 0

	var workers sync.WaitGroup
	var task sync.WaitGroup

	// arbirarty number of workers with highest throughput tested for task
	for id := 0; id < numberOfWorkers; id++ {
		workers.Add(1)
		go func(id int) {
			if verboseLogging {
				log.Println("starting worker", id)
			}
			worker(id, roleQueue, client, credentialsQueue, refreshCredentials, verboseLogging)
			defer workers.Done()
		}(id)
	}

	chunks := chunker(*verbose, accounts, batchSize)

	/*
		create batches, for accounts per batch create a task
		for each item of the batch, find a pointer to it's entry
		extra work has been put into chunking to add future tunables
	*/

	for _, chunk := range chunks {

		// for each entry in the chunk
		for i := 0; i < len(chunk); i++ {
			task.Add(1)
			for accountidx, account := range accounts {
				if strings.Contains(chunk[i].Id, account.Id) {
					go func(id int, chunk []Account, idx int) {
						uniqueAccountsProcessed++
						getAccountRolesConcurrent(chunk[id].Id, client, &accounts[idx], roleQueue, verboseLogging)
						defer task.Done()
					}(i, chunk, accountidx)
				}
			}
		}
	}

	task.Wait()
	close(roleQueue)

	workers.Wait()
	fmt.Println()

	return accounts
}

// Parallel worker to process known roles and fetch their credential details
func CredentialsWorker(id int, roleQueue chan Role, client *Client, CredCh chan Credential, refreshCredentials bool, verboseLogging bool) {
	var credential Credential
	// range won't break on len(ch) == 0 must be closed
	// role has a pointer to the underlying account to which it belongs
	for role := range roleQueue {

		roleName := fmt.Sprintf("%v-%v", role.Account.Name, role.Name)
		if verboseLogging {
			fmt.Printf("\r [worker-%d] pulling %v\n", id, roleName)
		} else {
			fmt.Printf("\033[2K") // clear line using ascii control chars
			fmt.Printf("\r [worker-%d] pulling %v", id, roleName)
		}

		if refreshCredentials {
			if verboseLogging {
				log.Println("fetching credentails for role", role)
			}
			credential = getAccountRoleCredentials(role, client)
		}

		// indirectly update the account to which the role belongs with the role details
		role.Credentials = credential
		role.Account.Roles = append(role.Account.Roles, role)
	}
}

// Parallel worker to list known profiles
func ProfilesWorker(id int, roleQueue chan Role, client *Client, CredCh chan Credential, refreshCredentials bool, verboseLogging bool) {

	// range won't break on len(ch) == 0 must be closed
	// role has a pointer to the underlying account to which it belongs
	for role := range roleQueue {
		// artificial delay since no processing done on the profiles
		time.Sleep(100 * time.Millisecond)
		roleName := fmt.Sprintf("%v-%v", role.Account.Name, role.Name)
		fmt.Println(roleName)
	}
}

// Get all roles returning read only roles for each account
func getAccountRolesConcurrent(accountId string, aws *Client, accountptr *Account, ch chan<- Role, verbose bool) {
	// returns all roles for accountId

	var err error
	var listAccountRolesOutput *sso.ListAccountRolesOutput
	var tooManyRequests *types.TooManyRequestsException

	for index, backoff := range backoffSchedule {
		// off set by 1
		attempts := index + 1

		listAccountRolesOutput, err = aws.Client.ListAccountRoles(context.TODO(), &sso.ListAccountRolesInput{
			AccessToken: &aws.Token.AccessToken,
			AccountId:   &accountId,
		})

		if err == nil {
			break
		}

		if errors.As(err, &tooManyRequests) {
			if verbose {
				log.Printf("\nBacking off ListAccountRoles request rate for %v attemps: %v of 3\n", accountId, attempts)
			}
			time.Sleep(backoff)
		}

	}

	// all retries failed
	check(err)

	// return each role as channel entry
	for _, role := range listAccountRolesOutput.RoleList {
		if strings.Contains(*role.RoleName, "Read") || strings.Contains(*role.RoleName, "Contributor") {
			ch <- Role{*role.RoleName, Credential{}, accountptr}
		}
	}
}

// Populate the sso credentials for each aws role assigned to an account
func getAccountRoleCredentials(r Role, aws *Client) Credential {
	listRolesCredentialsOutput, err := aws.Client.GetRoleCredentials(context.TODO(), &sso.GetRoleCredentialsInput{
		AccessToken: &aws.Token.AccessToken,
		AccountId:   &r.Account.Id,
		RoleName:    &r.Name,
	})
	check(err)
	return Credential{
		AccessKeyId:     *listRolesCredentialsOutput.RoleCredentials.AccessKeyId,
		SecretAccessKey: *listRolesCredentialsOutput.RoleCredentials.SecretAccessKey,
		SessionToken:    *listRolesCredentialsOutput.RoleCredentials.SessionToken,
	}
}
